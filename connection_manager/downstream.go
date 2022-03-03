package connection_manager

import (
	"container/ring"
	"context"
	"fmt"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p-core/event"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var log = logging.Logger("connection_manager")

// Downstream holds the downstream peers that are behind the proxy
type Downstream struct {
	host     host.Host
	ctx      context.Context
	peerRing *ring.Ring
	peers    map[ma.Multiaddr]bool
}

// NewDownstream creates a new Downstream struct
func NewDownstream(host host.Host, ctx context.Context, items ...ma.Multiaddr) (*Downstream, error) {
	peers := make(map[ma.Multiaddr]bool)

	d := &Downstream{
		host:     host,
		ctx:      ctx,
		peerRing: ring.New(len(items)),
		peers:    peers,
	}

	for i := 0; i < d.peerRing.Len(); i++ {
		d.peerRing.Value = items[i]
		d.peerRing = d.peerRing.Next()

		peers[items[i]] = true
	}

	return d, nil
}

// String gives a string representation of all the peers
func (d *Downstream) String() string {
	s := ""
	d.peerRing.Do(func(elem interface{}) {
		s += elem.(fmt.Stringer).String()
		s += " "
	})

	return s
}

// Next gets the next peer to connect to. At the moment it does round-robin
func (d *Downstream) Next() ma.Multiaddr {
	// Do we need locking? Worst thing that can happen is 2 or more upstream peers connect at the
	// same time, they connect to the same downstream peer, and the next one gets skipped. I am
	// not sure that the performance penalty for locking is actually worth it.
	var addr ma.Multiaddr

	// Save the current one in case no downstream hosts are available
	current := d.peerRing.Value.(ma.Multiaddr)

	for {
		addr = d.peerRing.Value.(ma.Multiaddr)
		d.peerRing = d.peerRing.Next()

		_, peer := peer.SplitAddr(addr)
		if d.host.Network().Connectedness(peer) == network.Connected {
			break
		} else if addr == current {
			log.Warn("All downstream peers are disconnected. Waiting for a few seconds before retrying.")
			// TODO Make configurable, possibly backoff
			time.Sleep(5 * time.Second)
		}

	}
	return addr
}

// Contains returns true if the given multiaddr is one of the downstream peers
func (d *Downstream) Contains(addr ma.Multiaddr) bool {
	enabled, found := d.peers[addr]
	return enabled && found
}

// ContainsPeer returns true if the given peer ID is one of the downstream peers
func (d *Downstream) ContainsPeer(id peer.ID) bool {
	for addr := range d.peers {
		_, peerId := peer.SplitAddr(addr)
		if id == peerId {
			return true
		}
	}
	return false
}

// Peers returns a slice containing all the downstream peer multiaddrs
func (d *Downstream) Peers() []ma.Multiaddr {
	peers := make([]ma.Multiaddr, 0, len(d.peers))
	for id := range d.peers {
		peers = append(peers, id)
	}
	return peers
}

// ConnectAll connects to all the downstream peers and establishes a background goroutine
// to reconnect when they disconnect
func (d *Downstream) ConnectAll(h *host.Host, ctx context.Context) {
	go reconnect(d, ctx)

	d.peerRing.Do(func(target interface{}) {
		log.Debugf("Connecting to downstream peer %v", target)
		peerInfo, err := peer.AddrInfoFromP2pAddr(target.(ma.Multiaddr))
		if err != nil {
			log.Errorf("Error while parsing multiaddr %s: %s", target, err)
		} else {
			log.Debugf("Connecting to peer %v", peerInfo)
			go d.reconnectLoop(*peerInfo)
		}
	})
}

// reconnect is the goroutine that reconnects whenever a peer is disconnected
func reconnect(d *Downstream, ctx context.Context) {
	ch, err := d.host.EventBus().Subscribe(&event.EvtPeerConnectednessChanged{})
	if err != nil {
		log.Fatalf("Error creating EventBus subscription: %v", err)
		return
	}

	log.Debug("Starting downstream host reconnection loop")
	for {
		evt := (<-ch.Out()).(event.EvtPeerConnectednessChanged)

		switch evt.Connectedness {
		case network.Connected:
			if d.ContainsPeer(evt.Peer) {
				log.Debugf("Connected to downstream peer %v", evt.Peer)
			}

		case network.NotConnected:
			// Only reconnect to downstream peers
			if d.ContainsPeer(evt.Peer) {
				info := d.host.Peerstore().PeerInfo(evt.Peer)
				log.Debugf("Downstream peer %v disconnected, reconnecting", info)
				go d.reconnectLoop(info)
			}

		default:
			log.Debugf("Received event: %v", evt)
		}
	}
}

// reconnectLoop is a goroutine that periodically tries to reconnect to a disconnected peer
func (d *Downstream) reconnectLoop(info peer.AddrInfo) {
	log.Debug("Starting reconnection loop for peer", info)
	for {
		err := d.host.Connect(d.ctx, info)
		if err == nil {
			break
		}

		log.Errorf("Failed to reconnect to downstream peer %v. Attempting reconnection. Error: %s", info, err)
		// TODO Make configurable, perhaps backoff?
		time.Sleep(5 * time.Second)
	}
}
