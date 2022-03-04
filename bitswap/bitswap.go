package bitswap

import (
	"context"
	"fmt"
	"io"

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

// Handle a connection from a from a downstream peer (which will be in reply to a WANT message
// from an upstream peer)
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
				log.Errorf("Error while reading Bitswap message from %v: %v\n", downPeer, err)
			}
			return
		}

		// TODO Handle received.Haves()
		// TODO For received.DontHaves(), use GET /api/v0/refs?arg=CID&recursive=true to the downstream host
		for _, block := range received.Blocks() {
			cid := block.Cid()
			upPeers := connMgr.GetWantingPeers(cid)
			log.Debugf("Received block %v wanted by %v\n", cid, upPeers)

			for _, upPeer := range upPeers {
				upStream, found := streamMap[upPeer]
				if !found {
					log.Debugf("Creating a new stream to %v\n", upPeer)

					upStream, err = ha.NewStream(context.Background(), upPeer, proto)
					if err != nil {
						log.Warnf("Error while creating a new stream to upstream %v: %v\n", upPeer, err)
						continue
					}

					streamMap[upPeer] = upStream
				}

				// Sending the blocks one by one is slower but simpler and uses less memory,
				// since each block we receive might be wanted by multiple peers (we would
				// have to build a message for each of the peers and send them in one go)
				// For the purposes of the preloads this should be fine (fingers crossed)
				msg := bsmsg.New(false)
				msg.AddBlock(block)

				err = sendBitswapMessage(proto, msg, upStream)
				if err != nil {
					log.Warnf("Error while sending Bitswap message to %v: %v\n", upPeer, err)
				}
			}
		}
	}
}

// Handle an incoming stream from an upstream peer
func handleUpStream(ha host.Host, connMgr *cm.ConnectionManager, upStream network.Stream) {
	defer upStream.Close()

	// Some of this adapted from go-bitswap/network/ipfs_impl.go#handleNewStream
	upPeer := upStream.Conn().RemotePeer()
	proto := upStream.Protocol()
	reader := msgio.NewVarintReaderSize(upStream, network.MessageSizeMax)

	downPeer := connMgr.GetDownstreamPeer(upPeer)

	log.Debugf("\nOpening bitswap stream: %v: %v -> %v\n", proto, upPeer, downPeer)
	downStream, err := ha.NewStream(context.Background(), downPeer, proto)

	if err != nil {
		log.Warnf("\nError creating stream %v: %v -> %v: %s\n", proto, upPeer, downPeer, err)
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

		wantlist := received.Wantlist()

		// TODO Send the wants to all the downstream hosts? How do we handle DONT_HAVES then?
		if received.Full() {
			connMgr.RemoveWants(upPeer)

			for _, want := range wantlist {
				cid := want.Cid
				connMgr.AddWant(upPeer, cid)
			}

			// TODO Possible race condition: a peer sends a Full Wantlist while another peer sends
			//      diff Wantlist. Need higher-level locking on the wantlist, or send a diff with
			//      just the new blocks. Sending Cancel messages would give us this race condition
			//      again, though
			// We will send a Full Wantlist, with everything that we want
			msg := bsmsg.New(true)

			for _, cid := range connMgr.GetWantedCids() {
				// TODO Perhaps ask for a DONT_HAVE and process that via /api/v0/refs?
				msg.AddEntry(cid, 0, bspb.Message_Wantlist_Block, false)
			}

			err = sendBitswapMessage(proto, msg, downStream)
			if err != nil {
				log.Warnf("Error while sending Bitswap Full Want message to %v: %s", downStream, err)
				continue
			}
		} else {
			for _, want := range wantlist {
				cid := want.Cid

				if want.Cancel {
					connMgr.RemoveWant(upPeer, cid)

					// Check if any other peer wants this CID
					wants := connMgr.GetWantingPeers(cid)
					log.Debugf("Peer %v does not want %v. It is now wanted by %v\n", upPeer, cid, wants)

					if len(wants) == 0 {
						// TODO Possible race condition: an upstream peer sends a Cancel while another one sends a Want
						err = sendBitswapMessage(proto, received, downStream)
						if err != nil {
							log.Debugf("Error while sending Bitswap Cancel message to %v: %s", downStream, err)
							continue
						}
					}
				} else {
					log.Debugf("Peer %v wants %v\n", upPeer, cid)
					connMgr.AddWant(upPeer, cid)
					log.Debugf("CID %v is now wanted by %v\n", cid, connMgr.GetWantingPeers(cid))

					err = sendBitswapMessage(proto, received, downStream)
					if err != nil {
						log.Warnf("Error while sending Bitswap Want message to %v: %s", downStream, err)
						continue
					}
				}
			}
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
