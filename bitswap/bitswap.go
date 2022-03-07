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

func AddBitswapHandler(h host.Host, connMgr *cm.ConnectionManager) {
	// It would be nice to make this more generic (i.e. adding other protocols)
	h.SetStreamHandler(ProtocolBitswap, bitswapHandler(h, connMgr))
	h.SetStreamHandler(ProtocolBitswapOneOne, bitswapHandler(h, connMgr))
	h.SetStreamHandler(ProtocolBitswapOneZero, bitswapHandler(h, connMgr))
	h.SetStreamHandler(ProtocolBitswapNoVers, bitswapHandler(h, connMgr))
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
func bitswapHandler(ha host.Host, connMgr *cm.ConnectionManager) func(s network.Stream) {
	return func(inStream network.Stream) {
		inPeer := inStream.Conn().RemotePeer()

		defer inStream.Close()

		if connMgr.IsDownstreamPeer(inPeer) {
			handleDownStream(ha, connMgr, inStream)
		} else {
			handleUpStream(ha, connMgr, inStream)
		}

	}
}

// Handle an incoming stream from a from a downstream peer (which will usually be in reply to a
// WANT message from an upstream peer)
func handleDownStream(ha host.Host, connMgr *cm.ConnectionManager, downStream network.Stream) {
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

		// TODO Handle received.Haves()
		handleBlocks(ha, connMgr, proto, &streamMap, &received)
		handleDontHaves(connMgr, &received, downPeer)
	}
}

func handleBlocks(
	h host.Host,
	connMgr *cm.ConnectionManager,
	proto protocol.ID,
	streamMap *map[peer.ID]network.Stream,
	received *bsmsg.BitSwapMessage) {
	var err error

	// TODO Should we cache the blocks for a short time in case we get a WANT for one of them?
	for _, block := range (*received).Blocks() {
		cid := block.Cid()
		upPeers := connMgr.GetWantingPeers(cid)
		log.Debugf("Received block %v wanted by %v", cid, upPeers)

		for _, upPeer := range upPeers {
			upStream, found := (*streamMap)[upPeer]
			if !found {
				log.Debugf("Creating a new stream to %v", upPeer)

				upStream, err = h.NewStream(context.Background(), upPeer, proto)
				if err != nil {
					log.Warnf("Error while creating a new stream to upstream %v: %v", upPeer, err)
					continue
				}

				(*streamMap)[upPeer] = upStream
			}

			// Sending the blocks one by one is slower but simpler and uses less memory, since each
			// block we receive might be wanted by multiple peers (we would have to build a message
			// for each of the peers and send them in one go). For the purposes of the preloads
			// this should be fine (fingers crossed)
			msg := bsmsg.New(false)
			msg.AddBlock(block)

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
	peerInfo, found := connMgr.GetDownPeerInfo(downPeer)
	if !found {
		log.Errorf("Peer %s not found. Not sending /api/v0/refs.", downPeer)
		return
	}

	for _, cid := range (*received).DontHaves() {
		log.Debugf("Downstream peer %s does not have %s", downPeer, cid)
		// TODO Do these in parallel. Also, should we preemptively do it as part of WANT handling?
		url := fmt.Sprintf("http://%s:%v/api/v0/refs?recursive=true&cid=%s", peerInfo.IP, peerInfo.HttpPort, cid)
		resp, err := http.Get(url)
		if err != nil || (*resp).StatusCode > 299 {
			log.Warnf("HTTP error while fetching %s: %s (error %s)", url, (*resp).StatusCode, err)
			continue
		}
		// TODO Do we need to issue the WANT again, perhaps after some time so the host has a
		// 		chance to get the blocks, or will the upstream node do it?
	}
}

// Handle an incoming stream from an upstream peer
func handleUpStream(ha host.Host, connMgr *cm.ConnectionManager, upStream network.Stream) {
	defer upStream.Close()

	// Some of this adapted from go-bitswap/network/ipfs_impl.go#handleNewStream
	upPeer := upStream.Conn().RemotePeer()
	proto := upStream.Protocol()
	reader := msgio.NewVarintReaderSize(upStream, network.MessageSizeMax)

	downPeer := connMgr.GetDownstreamForPeer(upPeer)

	log.Debugf("Opening bitswap stream: %v: %v -> %v", proto, upPeer, downPeer)
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

		handleWantlist(connMgr, proto, upPeer, received, downStream)
	}
}

func handleWantlist(
	connMgr *cm.ConnectionManager,
	proto protocol.ID,
	upPeer peer.ID,
	received bsmsg.BitSwapMessage,
	downStream network.Stream) {

	wantlist := received.Wantlist()

	// TODO Send the wants to all the downstream hosts? How do we handle DONT_HAVES then?
	if received.Full() {
		log.Debugf("peer %s full wantlist: %s", upPeer, wantlist)
		connMgr.RemoveWants(upPeer)

		for _, want := range wantlist {
			cid := want.Cid
			connMgr.AddWant(upPeer, cid)
		}

		// We will send a Full Wantlist, with everything that we want
		// TODO Possible race condition: a peer sends a Full Wantlist while another peer sends
		//      diff Wantlist. Need higher-level locking on the wantlist, or send a diff with
		//      just the new blocks. Sending a diff with Cancel messages would give us this race
		// 		condition again, though
		msg := bsmsg.New(true)

		for _, cid := range connMgr.GetWantedCids() {
			msg.AddEntry(cid, 0, bspb.Message_Wantlist_Block, true)
		}

		err := sendBitswapMessage(proto, msg, downStream)
		if err != nil {
			log.Warnf("Error while sending Bitswap Full Want message to %v: %v", downStream, err)
			return
		}
	} else {
		msg := bsmsg.New(false)

		for _, want := range wantlist {
			log.Debugf("peer %s wants %s", upPeer, want)
			cid := want.Cid

			if want.Cancel {
				connMgr.RemoveWant(upPeer, cid)

				// Check if any other peer wants this CID
				// TODO: This check is naive: it might be that some other peer wants it, but it's
				//       associated with a different downstream peer. In that case we should send a
				//       Cancel to this one.
				wants := connMgr.GetWantingPeers(cid)
				log.Debugf("Peer %v does not want %v. It is now wanted by %v", upPeer, cid, wants)

				if len(wants) == 0 {
					// TODO Possible race condition: an upstream peer sends a Cancel while another one sends a Want
					err := sendBitswapMessage(proto, received, downStream)
					if err != nil {
						log.Debugf("Error while sending Bitswap Cancel message to %v: %s", downStream, err)
					}
				}
				continue
			}

			log.Debugf("peer %s wants %s", upPeer, want)
			connMgr.AddWant(upPeer, cid)
			log.Debugf("Peer %v wants %v, now wanted by %v", upPeer, cid, connMgr.GetWantingPeers(cid))

			// Ask for a DONT_HAVE so handleDontHave kicks in if the downstream peer does not have it
			msg.AddEntry(cid, 0, bspb.Message_Wantlist_Block, true)
		}

		err := sendBitswapMessage(proto, msg, downStream)
		if err != nil {
			log.Warnf("Error while sending Bitswap Want message to %v: %s", downStream, err)
		}
	}
}

// Convert the message to the appropriate protocol version and resend it on a stream
func sendBitswapMessage(proto protocol.ID, msg bsmsg.BitSwapMessage, s network.Stream) error {
	switch proto {
	case ProtocolBitswapOneOne, ProtocolBitswap:
		if err := msg.ToNetV1(s); err != nil {
			log.Warnf("Error sending Bitswap 1.1 message: %s", err)
			return err
		}
	case ProtocolBitswapOneZero, ProtocolBitswapNoVers:
		if err := msg.ToNetV0(s); err != nil {
			log.Warnf("Error sending Bitswap 1.0 message: %s", err)
			return err
		}
	default:
		return fmt.Errorf("unrecognized protocol on remote: %s", s.Protocol())
	}
	return nil
}
