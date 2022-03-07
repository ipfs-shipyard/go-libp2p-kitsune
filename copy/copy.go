package copy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/protocol"

	logging "github.com/ipfs/go-log/v2"

	"github.com/mcamou/go-libp2p-kitsune/bitswap"
	cm "github.com/mcamou/go-libp2p-kitsune/connection_manager"
)

var log = logging.Logger("copy")

// copyMatcher is a protocol matcher that matches those protocols that can be directly forwarded
func CopyMatcher(proto string) bool {
	// We ignore these protocols since they are either handled by specific protocol handlers
	// or not used by the preloads.
	// The protocol will ONLY be ignored if the value is `true`!
	blocklist := map[string]bool{
		"/ipfs/lan/kad/1.0.0":                  true,
		"/libp2p/autonat/1.0.0":                true,
		"/libp2p/circuit/relay/0.1.0":          true,
		"/libp2p/circuit/relay/0.2.0/stop":     true,
		"/libp2p/dcutr":                        true,
		"/x/":                                  true,
		string(bitswap.ProtocolBitswap):        true,
		string(bitswap.ProtocolBitswapOneOne):  true,
		string(bitswap.ProtocolBitswapOneZero): true,
		string(bitswap.ProtocolBitswapNoVers):  true,
	}

	if disabled, found := blocklist[proto]; !found && !disabled {
		return true
	} else {
		log.Infof("Ignoring protocol %s\n", proto)
		return false
	}
}

// copyHandler copies protocol messages back and forth between an upstream and a downstream host
func CopyHandler(ha host.Host, connMgr *cm.ConnectionManager) func(s network.Stream) {
	return func(upStream network.Stream) {
		defer upStream.Close()

		upPeer := upStream.Conn().RemotePeer()
		proto := upStream.Protocol()

		if connMgr.IsDownstreamPeer(upPeer) {
			log.Warnf("Received non-bitswap stream %v from downstream peer %v\n. Bailing out.", proto, upPeer)
			return
		}

		downPeer := connMgr.GetDownstreamForPeer(upPeer)

		log.Debugf("Opening stream: %v: %v -> %v\n", proto, upPeer, downPeer)
		downStream, err := ha.NewStream(context.Background(), downPeer, proto)
		defer downStream.Close()

		if err != nil {
			log.Warnf("Error creating stream %v: %v -> %v: %s\n", proto, upPeer, downPeer, err)
			return
		}

		downCh := make(chan error, 1)
		upCh := make(chan error, 1)

		go copyStream(proto, "->", upStream, downStream, ">", downCh)
		go copyStream(proto, "<-", downStream, upStream, "<", upCh)

		select {
		case err := <-downCh:
			log.Infof("Downstream channel %v: %v -> %v closed (error: %v)", proto, upPeer, downPeer, err)
		case err := <-upCh:
			log.Infof("Upstream channel %v: %v <- %v closed (error: %v)", proto, upPeer, downPeer, err)
		}
	}
}

// copyStream copies all bytes from one stream to another
func copyStream(proto protocol.ID, direction string, in network.Stream, out network.Stream, statusChar string, ch chan error) {
	inPeer := in.Conn().RemotePeer()
	outPeer := out.Conn().RemotePeer()

	// TODO Tune buffering
	buf := make([]byte, 1024)
	written := int64(0)
	var err error

	// Adapted from io.copyBuffer
	for {
		nr, er := in.Read(buf)
		if nr > 0 {
			fmt.Fprintf(os.Stderr, "%v: %v %v %v: % x\n", proto, inPeer, direction, outPeer, buf[0:nr])
			nw, ew := out.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errors.New("invalid write result")
				}
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = errors.New("short write")
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}

	if err == nil {
		log.Debugf("Closing stream %v: %v %v %v", proto, inPeer, direction, outPeer)
	} else {
		log.Warnf("Error reading from %v %v: %v\n", proto, inPeer, err)
	}

	close(ch)
}
