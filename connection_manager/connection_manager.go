package connection_manager

import (
	"context"

	"github.com/ipfs/go-cid"

	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"

	ma "github.com/multiformats/go-multiaddr"

	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

type ConnectionManager struct {
	down    *Downstream
	connMap *bmm.BiMultiMap // upstream peer ID <-> downstream peer ID
	wantMap *bmm.BiMultiMap // CID <-> PeerIDs with wants
}

func New(host host.Host, ctx context.Context, addrs ...ma.Multiaddr) *ConnectionManager {
	down := newDownstream(host, ctx, addrs...)
	connMap := bmm.New()
	wantMap := bmm.New()

	return &ConnectionManager{down, connMap, wantMap}
}

func (cm *ConnectionManager) ConnectAllDown() {
	n := newNotifiee(cm.down, cm.connMap, cm.wantMap)
	cm.down.connectAll(n)
}

func (cm *ConnectionManager) IsDownstreamPeer(id peer.ID) bool {
	return cm.down.ContainsPeer(id)
}

func (cm *ConnectionManager) GetDownstreamPeer(upPeer peer.ID) peer.ID {
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

// TODO Refactor these out to a separate Wantlist struct (the main problem is that the Notifiee needs the Wantlist)
func (cm *ConnectionManager) GetWantingPeers(c cid.Cid) []peer.ID {
	values := cm.wantMap.GetValues(c)
	peers := make([]peer.ID, 0, len(values))

	for _, p := range values {
		peers = append(peers, p.(peer.ID))
	}

	return peers
}

func (cm *ConnectionManager) AddWant(p peer.ID, c cid.Cid) {
	cm.wantMap.Put(c, p)
}

func (cm *ConnectionManager) GetWantedCids() []cid.Cid {
	keys := cm.wantMap.Keys()
	cids := make([]cid.Cid, 0, len(keys))

	for _, c := range keys {
		cids = append(cids, c.(cid.Cid))
	}

	return cids
}

func (cm *ConnectionManager) RemoveWant(p peer.ID, c cid.Cid) {
	cm.wantMap.DeleteKeyValue(p, c)
}

func (cm *ConnectionManager) RemoveWants(p peer.ID) {
	cm.wantMap.DeleteKey(p)
}
