package connection_manager

import (
	"context"
	"net"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/peer"

	ma "github.com/multiformats/go-multiaddr"

	bmm "github.com/mcamou/go-libp2p-kitsune/bimultimap"
)

type ConnectionManager struct {
	down      *Downstream
	upIP      *bmm.BiMultiMap // upstream peer ID <-> IP address (as a string)
	conns     *bmm.BiMultiMap // upstream peer ID <-> downstream peer ID
	refReqs   *bmm.BiMultiMap // upstream IP (as a string) <-> CID (requested by /api/v0/refs)
	UpWants   *WantMap        // Wants by upstream peers
	DownWants *WantMap        // Wants by downstream peers (in response to /api/v0/refs from upstream)
	SentWants *WantMap        // All wants that have been sent, and the peer that they were sent to
}

func New(host host.Host, ctx context.Context, addrs ...ma.Multiaddr) (*ConnectionManager, error) {
	down, err := newDownstream(host, ctx, addrs...)
	if err != nil {
		return nil, err
	}

	conns := bmm.New()
	upIP := bmm.New()
	refReqs := bmm.New()
	upWants := NewWantMap()
	downWants := NewWantMap()
	sentWants := NewWantMap()

	return &ConnectionManager{down, upIP, conns, refReqs, upWants, downWants, sentWants}, nil
}

// ConnectAllDown connects to all downstream peers
func (cm *ConnectionManager) ConnectAllDown() {
	n := newNotifiee(cm)
	cm.down.connectAll(n)
}

// IsDownstream returns true if a peer is a downstream peer
func (cm *ConnectionManager) IsDownstream(id peer.ID) bool {
	return cm.down.ContainsPeer(id)
}

// DownstreamForPeer returns the downstream peer associated with an upstream peer. It returns a
// one-element array for symmetry with UpstreamForPeer.
func (cm *ConnectionManager) DownstreamForPeer(upPeer peer.ID) []peer.ID {
	downPeers := cm.conns.LookupKey(upPeer)

	// This should never happen, since the Notifiee will take care of filling in the connMap when
	// a peer connects. still, better safe than sorry
	if len(downPeers) == 0 {
		downPeer := cm.down.Next()
		cm.conns.Add(upPeer, downPeer)
		log.Debugf("Downstream peer not found for upstream %v, connecting to %v\n", upPeer, downPeer)

		return []peer.ID{downPeer}
	} else {
		return []peer.ID{downPeers[0].(peer.ID)}
	}
}

// CurrentDownPeer returns the last-selected downstream peer
func (cm *ConnectionManager) CurrentDownPeer() peer.ID {
	return cm.down.Current()
}

// DownPeerInfo returns the info of a downstream peer
func (cm *ConnectionManager) DownPeerInfo(id peer.ID) (PeerInfo, bool) {
	elem, found := cm.down.peers[id]
	if !found {
		log.Debugf("downstream peer %s not found in %v", id, cm.down.peers)
	}
	return elem, found
}

// DownPeers returns the multiaddrs of all downstream peers
func (cm *ConnectionManager) DownPeers() []ma.Multiaddr {
	return cm.down.PeerAddrs()
}

// UpPeers returns the peer IDs of all connected upstream peers
func (cm *ConnectionManager) UpPeers() []peer.ID {
	keys := cm.conns.Keys()
	peers := make([]peer.ID, 0, len(keys))
	for _, id := range keys {
		peers = append(peers, id.(peer.ID))
	}
	return peers
}

// AddUpstreamPeerIP adds a mapping between an upstream peer ID and its IP address
func (cm *ConnectionManager) AddUpstreamPeerIP(addr ma.Multiaddr) error {
	_, peerId := peer.SplitAddr(addr)
	ip, err := cm.IpFromMultiaddr(addr)

	if err != nil {
		return err
	}

	cm.upIP.Add(peerId, ip.String())

	return nil
}

// DeleteUpstreamPeerIP deletes one peerID <-> IP address association
func (cm *ConnectionManager) DeleteUpstreamPeerIP(id peer.ID) {
	cm.upIP.DeleteKey(id)
}

// UpstreamPeersForIP returns the upstream peers associated with a given IP. Note that there can be
// several in the case of peers behind a NAT.
func (cm *ConnectionManager) UpstreamPeersForIP(ip net.IP) []peer.ID {
	keys := cm.upIP.LookupValue(ip.String())
	peerIds := make([]peer.ID, 0, len(keys))
	for _, id := range keys {
		peerIds = append(peerIds, id.(peer.ID))
	}
	return peerIds
}

// UpstreamIPForPeer returns the upstream IP associated with a given peer.
func (cm *ConnectionManager) UpstreamIPForPeer(id peer.ID) (net.IP, bool) {
	keys := cm.upIP.LookupKey(id)

	if len(keys) > 0 {
		// Since this is set up when the peer actually connects, we know that there is a single IP
		// for this peer
		return net.ParseIP(keys[0].(string)), true
	}
	return nil, false
}

// AddRef adds an IP <-> CID mapping
func (cm *ConnectionManager) AddRefCid(ip net.IP, c cid.Cid) {
	cm.refReqs.Add(ip.String(), c)
}

// DeleteRefCid removes an IP <-> CID mapping
func (cm *ConnectionManager) DeleteRefCid(ip net.IP, c cid.Cid) {
	cm.refReqs.DeleteKeyValue(ip.String(), c)
}

// RefsForCid returns all the IPs that have requested the CID via /api/v0/refs
func (cm *ConnectionManager) RefsForCid(c cid.Cid) []net.IP {
	keys := cm.refReqs.LookupValue(c)
	ips := make([]net.IP, 0, len(keys))
	for _, ip := range keys {
		ips = append(ips, net.ParseIP(ip.(string)))
	}
	return ips
}

// CidsForRefIp returns all the CIDs that have been requested by the IP via /api/v0/refs
func (cm *ConnectionManager) CidsForRefIp(ip net.IP) []cid.Cid {
	values := cm.refReqs.LookupKey(ip)
	cids := make([]cid.Cid, 0, len(values))
	for _, c := range values {
		cids = append(cids, c.(cid.Cid))
	}
	return cids
}

// UpstreamForPeer returns all the upstream peers associated with a downstream peer
func (cm *ConnectionManager) UpstreamForPeer(id peer.ID) []peer.ID {
	keys := cm.conns.LookupValue(id)
	peers := make([]peer.ID, 0, len(keys))
	for _, p := range keys {
		peers = append(peers, p.(peer.ID))
	}
	return peers
}

// IpFromMultiaddr extracts the IP address from a multiaddr
func (cm *ConnectionManager) IpFromMultiaddr(addr ma.Multiaddr) (net.IP, error) {
	transport, _ := peer.SplitAddr(addr)

	ip, err := transport.ValueForProtocol(ma.ProtocolWithName("ip4").Code)
	if err != nil {
		ip, err = transport.ValueForProtocol(ma.ProtocolWithName("ip6").Code)
		if err != nil {
			return nil, err
		}
	}

	return net.ParseIP(ip), nil

}
