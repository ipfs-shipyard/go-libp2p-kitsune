package bitswap

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/libp2p/go-msgio"

	bsmsg "github.com/ipfs/go-bitswap/message"
	bspb "github.com/ipfs/go-bitswap/message/pb"
	logging "github.com/ipfs/go-log/v2"
	"github.com/ipfs/go-merkledag"

	pmgr "github.com/mcamou/go-libp2p-kitsune/peer_manager"
	"github.com/mcamou/go-libp2p-kitsune/prometheus"

	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

var log = logging.Logger("bitswap")

var (
	// ProtocolBitswapNoVers is equivalent to the legacy bitswap protocol
	ProtocolBitswapNoVers protocol.ID = "/ipfs/bitswap"
	// ProtocolBitswapOneZero is the protocol ID for the legacy bitswap protocol
	ProtocolBitswapOneZero protocol.ID = "/ipfs/bitswap/1.0.0"
	// ProtocolBitswapOneOne is the protocol ID for version 1.1.0
	ProtocolBitswapOneOne protocol.ID = "/ipfs/bitswap/1.1.0"
	// ProtocolBitswap is the current version of the bitswap protocol: 1.2.0
	ProtocolBitswap protocol.ID = "/ipfs/bitswap/1.2.0"
)

type Bitswap struct {
	host           host.Host
	peerManager    *pmgr.PeerManager
	enablePreload  bool
	wantTimestamps *bmm.BiMultiMap // CID <-> time WANT sent (for outstanding blocks)
}

func New(h host.Host, peerManager *pmgr.PeerManager, enablePreload bool) *Bitswap {
	return &Bitswap{h, peerManager, enablePreload, bmm.New()}
}

// AddHandler adds the handlers for the bitswap protocols
func (bs *Bitswap) AddHandler() {
	bs.host.SetStreamHandler(ProtocolBitswap, bs.handler)
	bs.host.SetStreamHandler(ProtocolBitswapOneOne, bs.handler)
	bs.host.SetStreamHandler(ProtocolBitswapOneZero, bs.handler)
	bs.host.SetStreamHandler(ProtocolBitswapNoVers, bs.handler)
}

// handler is a stream handler for the /ipfs/bitswap/* protocols
//
// At the moment it only handles the absolute minimum for the preload nodes to work
//
// WHY: In Bitswap, when a peer receives a WANT, it opens a new stream to send the BLOCKS messages
// instead of reusing the stream the requesting peer already opened. Therefore we need a special
// handler to keep track of which peer wants what and send it accordingly. See:
//   - https://www.notion.so/pl-strflt/Kitsune-a-libp2p-reverse-proxy-60df1d1a333646768951c2976e735234#1d0745f57281472a9c75a6c17164d9f5
//   - the Bitswap spec (https://github.com/ipfs/specs/blob/master/BITSWAP.md)
//   - the protobuf definition (https://github.com/ipfs/go-bitswap/blob/master/message/pb/message.proto)
func (bs *Bitswap) handler(inStream network.Stream) {
	defer inStream.Close()
	inPeer := inStream.Conn().RemotePeer()

	// TODO Should we only keep stats for downstream peers? There will be a LOT of upstream
	//      peers here
	prometheus.TotalBitswapMessagesRecv.Inc()
	prometheus.BitswapMessagesRecv.WithLabelValues(inPeer.String()).Inc()

	if bs.peerManager.IsDownstream(inPeer) {
		bs.handleDownStream(inPeer, &inStream)
	} else {
		bs.handleUpStream(inPeer, &inStream)
	}

}

// handleDownStream handles an incoming stream from a downstream peer
func (bs *Bitswap) handleDownStream(downPeer peer.ID, downStream *network.Stream) {
	// Some of this adapted from go-bitswap/network/ipfs_impl.go#handleNewStream
	proto := (*downStream).Protocol()

	log.Debugf("Opening bitswap stream from downstream %v", downPeer)

	reader := msgio.NewVarintReaderSize(*downStream, network.MessageSizeMax)

	for {
		received, err := bsmsg.FromMsgReader(reader)
		if err != nil {
			if err != io.EOF {
				log.Errorf("Error while reading Bitswap message from %v: %v", downPeer, err)
			}
			return
		}

		bs.handleBlocks(proto, downPeer, bs.peerManager.UpWants, &received)
		bs.handleDontHaves(proto, &received, downPeer)

		if bs.enablePreload {
			// A js-ipfs peer will call /api/v0/refs?recursive=true&cid=<CID> on the preload node,
			// to have it send WANTs for the js-ipfs node's data (so that it gets safely stored
			// in case of e.g. a page reload). Since the /api/v0/refs call does not include the
			// js-ipfs peer ID, we don't know where to send this WANT to. We use the peer's IP
			// address as a proxy for the peer ID, and match by the requested CID.
			//
			// The IP might map to several js-ipfs peers (e.g. if they are behind a NAT). In that
			// case, we will send WANTs to all of them, but only one of them should reply. However,
			// that is much better than just sending the WANTs to all upstream peers.
			wantedBy := pmgr.NewWantMap()
			for _, want := range received.Wantlist() {
				for _, ip := range bs.peerManager.RefsForCid(want.Cid) {
					for _, id := range bs.peerManager.UpstreamPeersForIP(ip) {
						wantedBy.Add(id, want.Cid)
					}
				}
			}

			bs.handleWantlist(proto, downPeer, received, bs.peerManager.DownWants, wantedBy, bs.peerManager.SentWants, bs.peerManager.UpstreamForPeer(downPeer))
		}
	}
}

// handleUpstream handles an incoming stream from an upstream peer
func (bs *Bitswap) handleUpStream(upPeer peer.ID, upStream *network.Stream) {
	// Reference: go-bitswap/network/ipfs_impl.go#handleNewStream
	proto := (*upStream).Protocol()
	reader := msgio.NewVarintReaderSize((*upStream), network.MessageSizeMax)

	downPeer := bs.peerManager.DownstreamForPeer(upPeer)[0]

	log.Debugf("Opening bitswap stream from upstream: %v: %v -> %v", proto, upPeer, downPeer)
	downStream, err := bs.host.NewStream(context.Background(), downPeer, proto)
	if err != nil {
		log.Warnf("Error creating stream %v: %v -> %v: %s", proto, upPeer, downPeer, err)
		return
	}
	defer downStream.Close()

	for {
		received, err := bsmsg.FromMsgReader(reader)

		if err != nil {
			if err != io.EOF {
				_ = (*upStream).Reset()
				log.Warnf("bitswap error from %s: %s", upPeer, err)
			}
			return
		}

		bs.handleWantlist(proto, upPeer, received, bs.peerManager.UpWants, nil, bs.peerManager.SentWants, bs.peerManager.DownstreamForPeer(upPeer))
		bs.handleDontHaves(proto, &received, downPeer)

		if bs.enablePreload {
			// Upstream peers send BLOCKS in response to the WANTs sent by the downstream peers in
			// response to /api/v0/refs (see #handleWantlist). We just forward them to the
			// corresponding downstream peer.
			bs.handleBlocks(proto, upPeer, bs.peerManager.DownWants, &received)
		}
	}
}

// handleBlocks handles BLOCKS messages. This is fairly straightforward since a BLOCKS message is
// always a reply to one or more WANT messages, only complicated by the fact that BLOCKS messages
// always come in a new stream (instead of in the same stream where the WANT was sent) and that a
// BLOCKS message from a downstream peer can contain blocks destined for different upstream peers
// (since the downstream peer only sees us)
func (bs *Bitswap) handleBlocks(
	proto protocol.ID,
	fromPeer peer.ID,
	wantMap *pmgr.WantMap,
	received *bsmsg.BitSwapMessage) {

	for _, block := range (*received).Blocks() {
		c := block.Cid()
		times := bs.wantTimestamps.DeleteKey(c)
		// TODO Should we only keep stats for downstream peers? There will be a LOT of upstream
		//      peers here
		if len(times) > 0 {
			elapsed := time.Since(times[0].(time.Time)).Milliseconds()
			prometheus.BlockRTTms.WithLabelValues(fromPeer.String()).Observe(float64(elapsed))
		}

		targetPeers := wantMap.PeersForCid(c)
		log.Debugf("Received block %s wanted by %s", c, targetPeers)

		for _, p := range targetPeers {
			log.Debugf("Creating a new stream to %v", p)

			s, err := bs.host.NewStream(context.Background(), p, proto)
			if err != nil {
				log.Warnf("Error while creating a new stream to upstream %v: %v", p, err)
				continue
			}
			defer s.Close()

			// Sending the messages one by one is slower but simpler and uses less memory, since each
			// block we receive might be wanted by multiple peers (we would have to build a message
			// for each of the peers and send them in one go). For the purposes of the preloads
			// this should be fine (fingers crossed)
			log.Debugf("Sending block %s to %s", c, p)
			msg := bsmsg.New(false)
			msg.AddBlock(block)
			bs.sendBitswapMessage(proto, &msg, p)

			// In preload mode, the downstream peer will probably issue WANTs for each of the links
			// in the block (since we call /api/v0/refs?recursive=true). We need to record these CIDs
			// to know to which upstream peer to route the WANTs. One issue is that the downstream
			// peer might already have the CID so it will not issue a WANT, so the CID will never be
			// removed from our map. However, all entries for the upstream peer will be cleared once
			// it disconnects.
			ip, found := bs.peerManager.UpstreamIPForPeer(p)
			if bs.enablePreload && found {
				node, err := merkledag.DecodeProtobufBlock(block)
				if err != nil {
					log.Warnf("Error decoding Merkledag for block %s: %s", c, err)
					continue
				}
				for _, link := range node.Links() {
					log.Debugf("Adding Ref for %s (child of %s) from %s", link.Cid, c, ip)
					bs.peerManager.AddRefCid(ip, link.Cid)
				}
			}
		}
	}
}

// handleDontHaves handles the DONT_HAVE message coming from a downstream peer. We use the hack
// that js-ipfs uses with the preloads: issue a GET /api/v0/refs?cid=CID&recursive=true to have
// the downstream node get the blocks from the rest of the network. We add recursive=true because
// in all probability the upstream peer will request the whole tree, so we save roundtrips.
//
// We don't forward the DONT_HAVE to the upstream node so that it eventually resends the WANT.
func (bs *Bitswap) handleDontHaves(
	proto protocol.ID,
	received *bsmsg.BitSwapMessage,
	peerId peer.ID) {
	if bs.peerManager.IsDownstream(peerId) {
		// Must be a downstream peer. Use the refs endpoint to ask it to get it from the swarm.
		peerInfo, found := bs.peerManager.DownPeerInfo(peerId)
		if !found {
			log.Warnf("Downstream peer %s not found", peerId)
			return
		}

		for _, cid := range (*received).DontHaves() {
			log.Debugf("Downstream peer %s does not have %s. Requesting via /api/v0/refs.", peerId, cid)

			// TODO Do these in parallel
			url := fmt.Sprintf("http://%s:%v/api/v0/refs?recursive=true&arg=%s", peerInfo.IP, peerInfo.HttpPort, cid)
			resp, err := http.Post(url, "application/json", nil)
			if err != nil || (*resp).StatusCode > 299 {
				log.Warnf("HTTP error while fetching %s: %v (error %s)", url, (*resp).StatusCode, err)
				continue
			}
		}
		return
	}

	// Must be an upstream peer. Ignore the DONT_HAVE so that it will retry (wait for the downstream
	// peer to get it)
	log.Debugf("Upstream peer %s does not have %s. Ignoring.")
}

// handleWantlist handles the WANT messages. This is complicated by a few things:
// - Full vs. diff wantlists.
// - Wants vs. Cancels.
// - Preload mode. In this case, we have a WANT from downstream and don't know which
//   upstream peer to send it to.
func (bs *Bitswap) handleWantlist(
	proto protocol.ID,
	sourcePeer peer.ID, // Peer that the message comes from
	received bsmsg.BitSwapMessage,
	wantMap *pmgr.WantMap, // Peers that have expressed interest in each CID
	sendWantsTo *pmgr.WantMap, // CID <-> Peer to send each CID's WANTs to. nil means send to all assignedPeers
	sentWants *pmgr.WantMap, // Wants that have been already sent (to process Cancel messages)
	assignedPeers []peer.ID) {

	wantlist := received.Wantlist()

	// FIXME There is a race condition here:
	//       1. A peer sends a diff wantlist at the same time that another one sends a full one
	//       2. A peer sends a Cancel at the same time that another one sends a WANT
	//       We might need to have a separate RWLock, or rethink how we handle Full wantlists
	//       wantMap.Lock()
	//       defer wantMap.Unlock()

	if received.Full() {
		bs.handleFullWantlist(proto, sourcePeer, wantlist, wantMap, assignedPeers)
	} else {
		msg := bsmsg.New(false)

		for _, want := range wantlist {
			c := want.Cid

			if want.Cancel {
				// Get a list of assigned peers to which this WANT was sent
				sentTo := make([]peer.ID, 0)
				for _, id := range assignedPeers {
					if len(sentWants.CidsForPeer(id)) > 0 {
						sentTo = append(sentTo, id)
					}
				}

				if len(sentTo) == 0 {
					continue
				}

				// Delete or record of this peer wanting this CID
				wantMap.Delete(sourcePeer, c)

				for _, id := range sentTo {
					sentWants.Delete(id, c)
				}

				// Cancel the WANT if nobody wants it any more
				// TODO This check is naive: It just gets all the peers that want this CID. Perhaps some
				//      other peers want it, but they are all associated with a different up/downstream
				//      peers. In that case we should still send a Cancel to this peer.
				peers := wantMap.PeersForCid(c)
				log.Debugf("Peer %v does not want %v. It is now wanted by %v", sourcePeer, c, peers)

				if len(peers) == 0 {
					msg := bsmsg.New(false)
					msg.Cancel(c)
					bs.sendBitswapMessages(proto, &msg, sentTo)
				}
			} else {
				wantMap.Add(sourcePeer, c)
				log.Debugf("Peer %v wants %v, now wanted by %v", sourcePeer, c, wantMap.PeersForCid(c))

				var peers []peer.ID
				if sendWantsTo == nil || len(sendWantsTo.PeersForCid(c)) == 0 {
					peers = assignedPeers
				} else {
					peers = sendWantsTo.PeersForCid(c)
				}

				// Ask for a DONT_HAVE so handleDontHave kicks in if the peer does not have it
				msg.AddEntry(c, 0, bspb.Message_Wantlist_Block, true)

				// Sending the messages one by one is slower but simpler and uses less memory, since
				// each WANT might correspond to multiple peers (we would have to build a message
				// for each of the peers and send them in one go). For the purposes of the preloads
				// this should be fine (fingers crossed)
				bs.sendBitswapMessages(proto, &msg, peers)
				bs.wantTimestamps.Add(c, time.Now())

				for _, id := range peers {
					sentWants.Add(id, c)
				}
			}
		}
	}
}

// handleFullWantlist handles a wantlist with the Full flag (i.e. it has the full set of WANTs that
// the remote peer is looking for, instead of just any new WANTs/Cancels)
func (bs *Bitswap) handleFullWantlist(
	proto protocol.ID,
	sourcePeer peer.ID,
	wantlist []bsmsg.Entry,
	wantMap *pmgr.WantMap,
	assigned []peer.ID) {
	log.Debugf("peer %s full wantlist: %v", sourcePeer, wantlist)

	// We should not forward a full wantlist from an upstream peer. We should extract only those
	// WANTs that we know the upstream peer already has (i.e., that have been requested via
	// /api/v0/refs)
	// TODO Is the above assumption true?
	if bs.peerManager.IsDownstream(sourcePeer) {
		return
	}

	wantMap.DeletePeer(sourcePeer)

	for _, want := range wantlist {
		cid := want.Cid
		wantMap.Add(sourcePeer, cid)
	}

	// We will send a Full Wantlist, with everything that we want
	msg := bsmsg.New(true)
	for _, cid := range wantMap.AllCids() {
		msg.AddEntry(cid, 0, bspb.Message_Wantlist_Block, true)
	}

	log.Debugf("Sending full wantlist msg to %s: %s", assigned, msg)
	bs.sendBitswapMessages(proto, &msg, assigned)
}

// sendBitswapMessage sends a bitswap message to multiple streams. peers is the list of peers to
// send to, if it's empty, send to all peers in the streams map
func (bs *Bitswap) sendBitswapMessages(proto protocol.ID, msg *bsmsg.BitSwapMessage, peers []peer.ID) {
	if len(peers) == 0 {
		log.Error("Sending bitswap message to no peers")
		debug.PrintStack()
	} else {
		for _, id := range peers {
			bs.sendBitswapMessage(proto, msg, id)
		}
	}
}

// sendBitswapMessage converts a message to the appropriate protocol version and sends it on a stream
func (bs *Bitswap) sendBitswapMessage(proto protocol.ID, msg *bsmsg.BitSwapMessage, id peer.ID) {
	log.Debugf("Sending bitswap message to %s", id)
	// TODO Should we only keep stats for downstream peers? There will be a LOT of upstream
	//      peers here
	prometheus.TotalBitswapMessagesSent.Inc()
	prometheus.BitswapMessagesSent.WithLabelValues(id.String()).Inc()

	s, err := bs.host.NewStream(context.Background(), id, proto)
	if err != nil {
		log.Warnf("Cannot open stream to %s", id)
		return
	}

	switch proto {
	case ProtocolBitswapOneOne, ProtocolBitswap:
		if err := (*msg).ToNetV1(s); err != nil {
			log.Warnf("Error sending Bitswap 1.1 message to peer %s: %s", id, err)
		}
	case ProtocolBitswapOneZero, ProtocolBitswapNoVers:
		if err := (*msg).ToNetV0(s); err != nil {
			log.Warnf("Error sending Bitswap 1.0 message to peer %s: %s", id, err)
		}
	default:
		err := fmt.Errorf("unrecognized protocol %s", proto)
		log.Warn(err)
	}
}
