package connection_manager

import (
	"context"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"

	ma "github.com/multiformats/go-multiaddr"

	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

type ConnectionManager struct {
	down        *Downstream
	connMap     *bmm.BiMultiMap // upstream peer ID <-> downstream peer ID
	upWantMap   *WantMap        // Wants by upstream peers
	downWantMap *WantMap        // Wants by downstream peers (in response to /api/v0/refs from upstream)
}

func New(host host.Host, ctx context.Context, addrs ...ma.Multiaddr) (*ConnectionManager, error) {
	down, err := newDownstream(host, ctx, addrs...)
	if err != nil {
		return nil, err
	}

	connMap := bmm.New()
	upWantMap := NewWantMap()
	downWantMap := NewWantMap()

	return &ConnectionManager{down, connMap, upWantMap, downWantMap}, nil
}

func (cm *ConnectionManager) ConnectAllDown() {
	n := newNotifiee(cm.down, cm.connMap, cm.upWantMap)
	cm.down.connectAll(n)
}

func (cm *ConnectionManager) IsDownstream(id peer.ID) bool {
	return cm.down.ContainsPeer(id)
}

func (cm *ConnectionManager) GetDownstreamForPeer(upPeer peer.ID) peer.ID {
	downPeers := cm.connMap.GetValues(upPeer)

	if len(downPeers) == 0 {
		downPeer := cm.down.Next()
		cm.connMap.Put(upPeer, downPeer)
		log.Debugf("Downstream peer not found for upstream %v, connecting to %v\n", upPeer, downPeer)

		return downPeer
	} else {
		return downPeers[0].(peer.ID)
	}
}

func (cm *ConnectionManager) GetCurrentDownPeer() peer.ID {
	return cm.down.Current()
}

func (cm *ConnectionManager) GetDownPeerInfo(id peer.ID) (PeerInfo, bool) {
	elem, found := cm.down.peers[id]
	if !found {
		log.Debugf("downstream peer %s not found in %v", id, cm.down.peers)
	}
	return elem, found
}

func (cm *ConnectionManager) DownPeers() []ma.Multiaddr {
	return cm.down.PeerAddrs()
}

func (cm *ConnectionManager) UpPeers() []peer.ID {
	keys := cm.connMap.Keys()
	peers := make([]peer.ID, 0, len(keys))
	for _, id := range keys {
		peers = append(peers, id.(peer.ID))
	}
	return peers
}

func (cm *ConnectionManager) UpstreamWantMap() *WantMap {
	return cm.upWantMap
}

func (cm *ConnectionManager) DownstreamWantMap() *WantMap {
	return cm.downWantMap
}
