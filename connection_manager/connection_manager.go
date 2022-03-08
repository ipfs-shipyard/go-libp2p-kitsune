package connection_manager

import (
	"context"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"

	ma "github.com/multiformats/go-multiaddr"

	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

type ConnectionManager struct {
	down    *Downstream
	connMap *bmm.BiMultiMap // upstream peer ID <-> downstream peer ID
	wantMap *WantMap
}

func New(host host.Host, ctx context.Context, addrs ...ma.Multiaddr) (*ConnectionManager, error) {
	down, err := newDownstream(host, ctx, addrs...)
	if err != nil {
		return nil, err
	}

	connMap := bmm.New()
	wantMap := newWantMap()

	return &ConnectionManager{down, connMap, wantMap}, nil
}

func (cm *ConnectionManager) ConnectAllDown() {
	n := newNotifiee(cm.down, cm.connMap, cm.wantMap)
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

func (cm *ConnectionManager) GetDownPeerInfo(id peer.ID) (PeerInfo, bool) {
	elem, found := cm.down.peers[id]
	if !found {
		log.Debugf("downstream peer %s not found in %v", id, cm.down.peers)
	}
	return elem, found
}

func (cm *ConnectionManager) WantMap() *WantMap {
	return cm.wantMap
}
