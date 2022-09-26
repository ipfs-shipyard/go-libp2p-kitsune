package peer_manager

import (
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	bmm "github.com/mcamou/go-bimultimap"
)

// WantMap is a wrapper around a peerId <-> CID BiMultiMap
type WantMap struct {
	wantMap *bmm.BiMultiMap[cid.Cid, peer.ID]
}

func NewWantMap() *WantMap {
	return &WantMap{wantMap: bmm.New[cid.Cid, peer.ID]()}
}
func (wm *WantMap) Merge(other *WantMap) *WantMap {
	return &WantMap{wantMap: wm.wantMap.Merge(other.wantMap)}
}

func (wm *WantMap) PeersForCid(c cid.Cid) []peer.ID {
	return wm.wantMap.LookupKey(c)
}

func (wm *WantMap) CidsForPeer(id peer.ID) []cid.Cid {
	return wm.wantMap.LookupValue(id)
}

func (wm *WantMap) AllCids() []cid.Cid {
	return wm.wantMap.Keys()
}

func (wm *WantMap) AllPeers() []peer.ID {
	return wm.wantMap.Values()
}

func (wm *WantMap) Add(p peer.ID, c cid.Cid) {
	wm.wantMap.Add(c, p)
}

func (wm *WantMap) Delete(p peer.ID, c cid.Cid) {
	wm.wantMap.DeleteKeyValue(c, p)
}

func (wm *WantMap) DeletePeer(p peer.ID) {
	wm.wantMap.DeleteValue(p)
}

func (wm *WantMap) DeleteCid(c cid.Cid) {
	wm.wantMap.DeleteKey(c)
}

func (wm *WantMap) Clear() {
	wm.wantMap.Clear()
}
