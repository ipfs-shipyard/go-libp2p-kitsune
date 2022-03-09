package connection_manager

import (
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

// Wrapper around a peerId <-> CID BiMultiMap
type WantMap struct {
	wantMap *bmm.BiMultiMap
}

func NewWantMap() *WantMap {
	return &WantMap{wantMap: bmm.New()}
}

func (wm *WantMap) GetPeers(c cid.Cid) []peer.ID {
	values := wm.wantMap.GetValues(c)
	peers := make([]peer.ID, 0, len(values))

	for _, p := range values {
		peers = append(peers, p.(peer.ID))
	}

	return peers
}

func (wm *WantMap) GetCids() []cid.Cid {
	keys := wm.wantMap.Keys()
	cids := make([]cid.Cid, 0, len(keys))

	for _, c := range keys {
		cids = append(cids, c.(cid.Cid))
	}

	return cids
}

func (wm *WantMap) Add(p peer.ID, c cid.Cid) {
	wm.wantMap.Put(c, p)
}

func (wm *WantMap) Delete(p peer.ID, c cid.Cid) {
	wm.wantMap.DeleteKeyValue(c, p)
}

func (wm *WantMap) DeletePeer(p peer.ID) {
	wm.wantMap.DeleteKey(p)
}

func (wm *WantMap) DeleteCid(c cid.Cid) {
	wm.wantMap.DeleteValue(c)
}

func (wm *WantMap) Lock() {
	wm.wantMap.Lock()
}

func (wm *WantMap) Unlock() {
	wm.wantMap.Unlock()
}
