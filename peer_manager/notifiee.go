package peer_manager

import (
	"fmt"

	"github.com/libp2p/go-libp2p-core/network"
	"github.com/mcamou/go-libp2p-kitsune/prometheus"
	ma "github.com/multiformats/go-multiaddr"
)

type Notifiee struct {
	peerManager *PeerManager
}

func newNotifiee(peerManager *PeerManager) *Notifiee {
	return &Notifiee{peerManager}
}

func (n *Notifiee) Listen(net network.Network, addr ma.Multiaddr) { // called when network starts listening on an addr
	log.Debugf("Started listening on %s", addr)
}

func (n *Notifiee) ListenClose(net network.Network, addr ma.Multiaddr) { // called when network stops listening on an addr
	log.Debugf("Stopped listening on %s", addr)
}

func (n *Notifiee) Connected(net network.Network, conn network.Conn) { // called when a connection opened
	peerId := conn.RemotePeer()
	p := fmt.Sprintf("/p2p/%s", peerId)
	addr, err := ma.NewMultiaddr(p)
	if err != nil {
		log.Errorf("Invalid multiaddr %s", p)
		return
	}

	remoteAddr := conn.RemoteMultiaddr().Encapsulate(addr)

	if n.peerManager.down.ContainsPeer(peerId) {
		// Downstream peer connected
		prometheus.CurrentDownstreamPeers.Add(1)
		log.Debugf("Connected to downstream peer %s", remoteAddr)
	} else {
		// Upstream peer connected
		prometheus.CurrentUpstreamPeers.Add(1)
		log.Debugf("Connected to upstream peer %s", remoteAddr)

		downPeer := n.peerManager.down.Next()
		n.peerManager.conns.Add(peerId, downPeer)
		err := n.peerManager.AddUpstreamPeerIP(remoteAddr)
		if err != nil {
			log.Warnf("Error adding upstream peer IP: %s", err)
		}
		log.Debugf("Connected to upstream peer %s <-> %s", peerId, downPeer)
	}
}

func (n *Notifiee) Disconnected(net network.Network, conn network.Conn) { // called when a connection closed
	remotePeer := conn.RemotePeer()
	info := n.peerManager.down.host.Peerstore().PeerInfo(remotePeer)

	if n.peerManager.down.ContainsPeer(remotePeer) {
		// Downstream peer disconnected
		prometheus.CurrentDownstreamPeers.Sub(1)
		// Disconnect from all upstream peers that are associated with that downstream peer (they
		// will reconnect and get assigned another one)
		upPeers := n.peerManager.conns.DeleteValue(remotePeer)
		for _, id := range upPeers {
			err := n.peerManager.down.host.Network().ClosePeer(id)
			if err != nil {
				log.Warnf("Error disconnecting from downstream peer %s: %s", id, err)
			}
		}

		log.Debugf("Downstream peer %v disconnected, reconnecting", info.ID)
		go n.peerManager.down.connectLoop(info)
	} else {
		// Upstream peer disconnected
		prometheus.CurrentUpstreamPeers.Sub(1)
		ip, found := n.peerManager.UpstreamIPForPeer(remotePeer)
		if found {
			for _, c := range n.peerManager.DownWants.CidsForPeer(remotePeer) {
				// TODO What if 2 peers behind the same NAT have requested the same CID?
				n.peerManager.DeleteRefCid(ip, c)
			}
		}
		n.peerManager.conns.DeleteKey(remotePeer)
		n.peerManager.DeleteUpstreamPeerIP(remotePeer)

		// TODO Send Cancels for all of this peer's CIDs to all downstream peers if nobody wants
		//      them any more
		n.peerManager.UpWants.DeletePeer(remotePeer)
		log.Debugf("Upstream peer %v disconnected", info.ID)
	}
}

func (n *Notifiee) OpenedStream(net network.Network, s network.Stream) {} // called when a stream opened

func (n *Notifiee) ClosedStream(net network.Network, s network.Stream) {} // called when a stream closed
