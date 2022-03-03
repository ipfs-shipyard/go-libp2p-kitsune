package connection_manager

import (
	"github.com/libp2p/go-libp2p-core/network"
	ma "github.com/multiformats/go-multiaddr"
)

type Notifiee struct {
	down *Downstream
}

func NewNotifiee(d *Downstream) *Notifiee {
	return &Notifiee{down: d}
}

func (n *Notifiee) Listen(net network.Network, addr ma.Multiaddr) { // called when network starts listening on an addr
	log.Debugf("Started listening on %s", addr)
}

func (n *Notifiee) ListenClose(net network.Network, addr ma.Multiaddr) { // called when network stops listening on an addr
	log.Debugf("Stopped listening on %s", addr)
}

func (n *Notifiee) Connected(net network.Network, conn network.Conn) { // called when a connection opened
	peer := conn.RemotePeer()
	if n.down.ContainsPeer(peer) {
		log.Debugf("Connected to downstream peer: %s", conn.RemoteMultiaddr())
	} else {
		log.Debugf("Connected to upstream peer: %s", conn.RemoteMultiaddr())
	}
}

func (n *Notifiee) Disconnected(net network.Network, conn network.Conn) { // called when a connection closed
	peer := conn.RemotePeer()
	info := n.down.host.Peerstore().PeerInfo(peer)

	if n.down.ContainsPeer(peer) {
		log.Debugf("Downstream peer %v disconnected, reconnecting", info.ID)
		go n.down.connectLoop(info)
	} else {
		log.Debugf("Upstream peer %v disconnected", info.ID)
	}
}

func (n *Notifiee) OpenedStream(net network.Network, s network.Stream) { // called when a stream opened
	log.Debugf("Open stream to peer %s", s.Conn().RemotePeer())
}

func (n *Notifiee) ClosedStream(net network.Network, s network.Stream) { // called when a stream closed
	log.Debugf("Closed stream to peer %s for protocol %s", s.Conn().RemotePeer(), s.Protocol())
}
