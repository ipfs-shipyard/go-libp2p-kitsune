package connection_manager

import (
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"

	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

type Notifiee struct {
	down    *Downstream
	connMap *bmm.BiMultiMap
	wantMap *WantMap
}

func newNotifiee(down *Downstream, connMap *bmm.BiMultiMap, wantMap *WantMap) *Notifiee {
	return &Notifiee{down, connMap, wantMap}
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
	remotePeer := conn.RemotePeer()
	info := n.down.host.Peerstore().PeerInfo(remotePeer)

	if n.down.ContainsPeer(remotePeer) {
		// Disconnect from all upstream peers that are associated with that downstream peer (they
		// will reconnect and get assigned another one)
		upPeers := n.connMap.DeleteValue(remotePeer)
		for _, id := range upPeers {
			n.down.host.Network().ClosePeer(id.(peer.ID))
		}

		log.Debugf("Downstream peer %v disconnected, reconnecting", info.ID)
		go n.down.connectLoop(info)
	} else {
		n.connMap.DeleteKey(remotePeer)

		// Delete all wants from that peer (upstream peers will re-request them after they reconnect)
		n.wantMap.DeletePeer(remotePeer)
		log.Debugf("Upstream peer %v disconnected", info.ID)
	}
}

func (n *Notifiee) OpenedStream(net network.Network, s network.Stream) {} // called when a stream opened

func (n *Notifiee) ClosedStream(net network.Network, s network.Stream) {} // called when a stream closed
