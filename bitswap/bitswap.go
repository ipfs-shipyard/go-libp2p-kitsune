package bitswap

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	msgio "github.com/libp2p/go-msgio"

	bsmsg "github.com/ipfs/go-bitswap/message"
	bspb "github.com/ipfs/go-bitswap/message/pb"
	logging "github.com/ipfs/go-log/v2"

	cm "github.com/mcamou/go-libp2p-kitsune/connection_manager"
)

var log = logging.Logger("bitswap")

var (
	// ProtocolBitswapNoVers is equivalent to the legacy bitswap protocol
	ProtocolBitswapNoVers protocol.ID = "/ipfs/bitswap"
	// ProtocolBitswapOneZero is the prefix for the legacy bitswap protocol
	ProtocolBitswapOneZero protocol.ID = "/ipfs/bitswap/1.0.0"
	// ProtocolBitswapOneOne is the the prefix for version 1.1.0
	ProtocolBitswapOneOne protocol.ID = "/ipfs/bitswap/1.1.0"
	// ProtocolBitswap is the current version of the bitswap protocol: 1.2.0
	ProtocolBitswap protocol.ID = "/ipfs/bitswap/1.2.0"
)

func AddBitswapHandler(h host.Host, connMgr *cm.ConnectionManager, enablePreload bool) {
	// It would be nice to make this more generic (i.e. adding other protocols)
	h.SetStreamHandler(ProtocolBitswap, bitswapHandler(h, connMgr, enablePreload))
	h.SetStreamHandler(ProtocolBitswapOneOne, bitswapHandler(h, connMgr, enablePreload))
	h.SetStreamHandler(ProtocolBitswapOneZero, bitswapHandler(h, connMgr, enablePreload))
	h.SetStreamHandler(ProtocolBitswapNoVers, bitswapHandler(h, connMgr, enablePreload))
}

// Stream handler for the /ipfs/bitswap/* protocols
//
// At the moment it only handles the absolute minimum for the preload nodes to work
//
// WHY: In Bitswap the downstream peer opens a new stream to the upstream peer to send the BLOCKS
// messages. Therefore we need a special handler to keep track of which upstream peer wants what
// and send it accordingly. See:
//
//   - https://www.notion.so/pl-strflt/Kitsune-a-libp2p-reverse-proxy-60df1d1a333646768951c2976e735234#1d0745f57281472a9c75a6c17164d9f5
//   - the Bitswap spec (https://github.com/ipfs/specs/blob/master/BITSWAP.md)
//   - the protobuf definition (https://github.com/ipfs/go-bitswap/blob/master/message/pb/message.proto)
//
// Some open questions:
//   - Do we need to create a new stream to the upstream peer or can we reuse the one we already have?
//   - If so, do we need to create a new stream for each block?
func bitswapHandler(ha host.Host, connMgr *cm.ConnectionManager, enablePreload bool) func(s network.Stream) {
	return func(inStream network.Stream) {
		inPeer := inStream.Conn().RemotePeer()

		defer inStream.Close()

		if connMgr.IsDownstream(inPeer) {
			handleDownStream(ha, connMgr, enablePreload, inStream)
		} else {
			handleUpStream(ha, connMgr, enablePreload, inStream)
		}

	}
}

// Handle an incoming stream from a from a downstream peer
func handleDownStream(ha host.Host, connMgr *cm.ConnectionManager, enablePreload bool, downStream network.Stream) {
	// Some of this adapted from go-bitswap/network/ipfs_impl.go#handleNewStream
	defer downStream.Close()

	downPeer := downStream.Conn().RemotePeer()
	proto := downStream.Protocol()

	log.Debugf("Opening bitswap stream from downstream %v", downPeer)

	reader := msgio.NewVarintReaderSize(downStream, network.MessageSizeMax)

	streamMap := make(map[peer.ID]network.Stream)
	defer func() {
		for _, stream := range streamMap {
			stream.Close()
		}
	}()

	for {
		received, err := bsmsg.FromMsgReader(reader)
		if err != nil {
			if err != io.EOF {
				log.Errorf("Error while reading Bitswap message from %v: %v", downPeer, err)
			}
			return
		}

		handleBlocks(ha, connMgr, proto, &streamMap, connMgr.UpstreamWantMap(), &received)
		handleDontHaves(connMgr, &received, downPeer)

		// Downstream peers send WANTs in response to /api/v0/refs (only when preloads enabled)
		// Since ATM we don't know which upstream peer sent the /api/v0/refs request, we send
		// the WANT to all of them
		if enablePreload {
			// TODO This list might be large, we need to optimize by batching and/or weeding out
			//      the peer list, probably by IP (since we don't know which peer the request to
			//      /api/v0/refs came from)
			upPeers := connMgr.UpPeers()
			log.Debugf("Forwarding wantlist to upstream peers %s", upPeers)
			streams := make([]network.Stream, 0, len(upPeers))
			for _, peer := range upPeers {
				stream, err := ha.NewStream(context.Background(), peer, proto)
				if err != nil {
					log.Errorf("Error creating stream to upstream %s: %s", peer, err)
				}
				streams = append(streams, stream)
			}
			handleWantlist(connMgr, proto, downPeer, received, connMgr.DownstreamWantMap(), streams)
		}
	}
}

// Handle an incoming stream from an upstream peer
func handleUpStream(ha host.Host, connMgr *cm.ConnectionManager, enablePreload bool, upStream network.Stream) {
	defer upStream.Close()

	// Some of this adapted from go-bitswap/network/ipfs_impl.go#handleNewStream
	upPeer := upStream.Conn().RemotePeer()
	proto := upStream.Protocol()
	reader := msgio.NewVarintReaderSize(upStream, network.MessageSizeMax)

	downPeer := connMgr.DownstreamForPeer(upPeer)

	log.Debugf("Opening bitswap stream from upstream: %v: %v -> %v", proto, upPeer, downPeer)
	downStream, err := ha.NewStream(context.Background(), downPeer, proto)
	if err != nil {
		log.Warnf("Error creating stream %v: %v -> %v: %s", proto, upPeer, downPeer, err)
		return
	}

	for {
		received, err := bsmsg.FromMsgReader(reader)

		if err != nil {
			if err != io.EOF {
				_ = upStream.Reset()
				log.Warnf("bitswapHandler from %s error: %s", upPeer, err)
			}
			return
		}

		streams := []network.Stream{downStream}
		handleWantlist(connMgr, proto, upPeer, received, connMgr.UpstreamWantMap(), streams)

		// Upstream peers send BLOCKS in response to the WANTs sent by the downstream peers in
		// response to /api/v0/refs (see above in handleWantlist). We just forward them to the
		// corresponding downstream peer.
		if enablePreload {
			streamMap := make(map[peer.ID]network.Stream)
			handleBlocks(ha, connMgr, proto, &streamMap, connMgr.DownstreamWantMap(), &received)
		}

		for cid := range received.DontHaves() {
			log.Debugf("Upstream peer %s does not have %s", upPeer, cid)
		}

		// We don't care about DONT_HAVEs from upstream
	}
}

func handleBlocks(
	h host.Host,
	connMgr *cm.ConnectionManager,
	proto protocol.ID,
	streamMap *map[peer.ID]network.Stream,
	wantMap *cm.WantMap,
	received *bsmsg.BitSwapMessage) {
	var err error

	// TODO Should we cache the blocks for a short time in case we get a WANT for one of them?
	for _, block := range (*received).Blocks() {
		cid := block.Cid()
		upPeers := wantMap.PeersForCid(cid)
		log.Debugf("Received block %v wanted by %v", cid, upPeers)

		for _, upPeer := range upPeers {
			var upStream network.Stream
			found := false

			if streamMap != nil {
				upStream, found = (*streamMap)[upPeer]
			}
			if !found {
				log.Debugf("Creating a new stream to %v", upPeer)

				upStream, err = h.NewStream(context.Background(), upPeer, proto)
				if err != nil {
					log.Warnf("Error while creating a new stream to upstream %v: %v", upPeer, err)
					continue
				}

				if streamMap != nil {
					(*streamMap)[upPeer] = upStream
				}
			}

			// Sending the blocks one by one is slower but simpler and uses less memory, since each
			// block we receive might be wanted by multiple peers (we would have to build a message
			// for each of the peers and send them in one go). For the purposes of the preloads
			// this should be fine (fingers crossed)
			msg := bsmsg.New(false)
			msg.AddBlock(block)

			log.Debugf("Sending block %s to %s", cid, upPeer)
			err = sendBitswapMessage(proto, msg, upStream)
			if err != nil {
				log.Warnf("Error while sending Bitswap message to %v: %v", upPeer, err)
			}
		}
	}
}

// handleDontHaves handles the DONT_HAVE message coming from a downstream server. We use the hack
// that js-ipfs uses with the preloads: issue a GET /api/v0/refs?cid=CID&recursive=true to have
// the downstream node get the blocks from the rest of the network. We add recursive=true because
// in all probability the upstream peer will request the whole tree, so we save roundtrips.
//
// We don't forward the DONT_HAVE to the upstream node. The downstream node will eventually issue
// the WANT again.
func handleDontHaves(connMgr *cm.ConnectionManager, received *bsmsg.BitSwapMessage, downPeer peer.ID) {
	peerInfo, found := connMgr.DownPeerInfo(downPeer)
	if !found {
		log.Errorf("Peer %s not found. Not sending /api/v0/refs.", downPeer)
		return
	}

	for _, cid := range (*received).DontHaves() {
		log.Debugf("Downstream peer %s does not have %s", downPeer, cid)

		// TODO Do these in parallel. Should we preemptively do it as part of WANT handling?
		url := fmt.Sprintf("http://%s:%v/api/v0/refs?recursive=true&arg=%s", peerInfo.IP, peerInfo.HttpPort, cid)
		resp, err := http.Post(url, "application/json", nil)
		if err != nil || (*resp).StatusCode > 299 {
			log.Warnf("HTTP error while fetching %s: %s (error %s)", url, (*resp).StatusCode, err)
			continue
		}
	}
}

func handleWantlist(
	connMgr *cm.ConnectionManager,
	proto protocol.ID,
	sourcePeerID peer.ID,
	received bsmsg.BitSwapMessage,
	wantMap *cm.WantMap,
	destStreams []network.Stream) {
	// We need to lock the wantMap for 2 reasons:
	// 1. A peer sends a partial wantlist at the same time that another one sends a full one
	// 2. A peer sends a Cancel at the same time that another one sends a WANT
	wantMap.Lock()
	defer wantMap.Unlock()

	wantlist := received.Wantlist()

	// TODO Should we send the wants to all the downstream hosts? How do we handle DONT_HAVES then?
	if received.Full() {
		log.Debugf("peer %s full wantlist: %s", sourcePeerID, wantlist)

		wantMap.DeletePeer(sourcePeerID)

		for _, want := range wantlist {
			cid := want.Cid
			wantMap.Add(sourcePeerID, cid)
		}

		// We will send a Full Wantlist, with everything that we want
		msg := bsmsg.New(true)
		for _, cid := range wantMap.AllCids() {
			msg.AddEntry(cid, 0, bspb.Message_Wantlist_Block, true)
		}

		log.Debugf("Sending full wantlist msg: %s", msg)
		sendBitswapMessages(proto, msg, destStreams)
	} else {
		msg := bsmsg.New(false)

		for _, want := range wantlist {
			cid := want.Cid

			if want.Cancel {
				wantMap.Delete(sourcePeerID, cid)

				// Cancel the WANT if nobody wants it
				// TODO This check is naive: some other peer might want it, but it's associated
				//      with a different downstream peer. In that case we should send a Cancel
				//      to this downstream peer.
				wants := wantMap.PeersForCid(cid)
				log.Debugf("Peer %v does not want %v. It is now wanted by %v", sourcePeerID, cid, wants)

				if len(wants) == 0 {
					msg := bsmsg.New(false)
					msg.Cancel(cid)
					sendBitswapMessages(proto, msg, destStreams)
				}
			} else {
				wantMap.Add(sourcePeerID, cid)
				log.Debugf("Peer %v wants %v, now wanted by %v", sourcePeerID, cid, wantMap.PeersForCid(cid))

				// Ask for a DONT_HAVE so handleDontHave kicks in if the downstream peer does not have it
				msg.AddEntry(cid, 0, bspb.Message_Wantlist_Block, true)
			}
		}

		sendBitswapMessages(proto, msg, destStreams)
	}
}

// Sends a bitswap message to multiple streams
func sendBitswapMessages(proto protocol.ID, msg bsmsg.BitSwapMessage, streams []network.Stream) {
	for _, s := range streams {
		sendBitswapMessage(proto, msg, s)
	}
}

// Convert the message to the appropriate protocol version and resend it on a stream
func sendBitswapMessage(proto protocol.ID, msg bsmsg.BitSwapMessage, s network.Stream) error {
	log.Debugf("Sending bitswap message to %s: %s", s.Conn().RemotePeer(), msg)
	switch proto {
	case ProtocolBitswapOneOne, ProtocolBitswap:
		if err := msg.ToNetV1(s); err != nil {
			log.Warnf("Error sending Bitswap 1.1 message to peer %s: %s", s.Conn().RemotePeer(), err)
			return err
		}
	case ProtocolBitswapOneZero, ProtocolBitswapNoVers:
		if err := msg.ToNetV0(s); err != nil {
			log.Warnf("Error sending Bitswap 1.0 message to peer %s: %s", s.Conn().RemotePeer(), err)
			return err
		}
	default:
		return fmt.Errorf("Unrecognized protocol %s", proto)
	}
	return nil
}
