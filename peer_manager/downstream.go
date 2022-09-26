package peer_manager

import (
	"container/ring"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	logging "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var log = logging.Logger("peer_manager")

// Downstream holds the downstream peers that are behind the proxy
type Downstream struct {
	host     host.Host
	ctx      context.Context
	peerRing *ring.Ring
	peers    map[peer.ID]PeerInfo
}

type PeerInfo struct {
	ID       peer.ID
	Addr     ma.Multiaddr // Bitswap address
	IP       string       // Or hostname
	HttpPort uint16
}

func (pi *PeerInfo) String() string {
	return fmt.Sprintf("%s(%s:%v)", pi.Addr, pi.IP, pi.HttpPort)
}

type IdResponse struct {
	ID        string
	Addresses []string
}

// NewDownstream creates a new Downstream struct
func newDownstream(host host.Host, ctx context.Context, addrs ...ma.Multiaddr) (*Downstream, error) {
	peers := make(map[peer.ID]PeerInfo)
	peerRing := ring.New(len(addrs))

	for _, addr := range addrs {
		peerInfo, err := getPeerInfo(addr)
		if err != nil {
			return nil, err
		}

		peerRing.Value = peerInfo.Addr
		peerRing = peerRing.Next()

		peers[peerInfo.ID] = *peerInfo
	}

	return &Downstream{
		host,
		ctx,
		peerRing,
		peers,
	}, nil
}

// getPeerInfo returns the details of a peer given its API multiaddr, using /api/v0/id to get
// the info. The multiaddr must be /ip4 (not yet ip6 nor DNS)
func getPeerInfo(httpAddr ma.Multiaddr) (*PeerInfo, error) {
	var host string

	hostAddr, err := httpAddr.ValueForProtocol(ma.ProtocolWithName("ip4").Code)
	if err == nil {
		host = fmt.Sprintf("/ip4/%s", hostAddr)
	} else {
		hostAddr, err = httpAddr.ValueForProtocol(ma.ProtocolWithName("ip6").Code)
		if err == nil {
			host = fmt.Sprintf("/ip6/%s", hostAddr)
			hostAddr = "[" + hostAddr + "]"
		} else {
			hostAddr, err = httpAddr.ValueForProtocol(ma.ProtocolWithName("dns4").Code)
			if err == nil {
				host = fmt.Sprintf("/dns4/%s", hostAddr)
			} else {
				hostAddr, err = httpAddr.ValueForProtocol(ma.ProtocolWithName("dns6").Code)
				if err == nil {
					host = fmt.Sprintf("/dns6/%s", hostAddr)
				} else {
					return nil, fmt.Errorf("error while getting host address from multiaddr %s", httpAddr)
				}
			}
		}
	}

	portStr, err := httpAddr.ValueForProtocol(ma.ProtocolWithName("tcp").Code)
	if err != nil {
		log.Errorf("Error while getting TCP port for multiaddr %s: %s", httpAddr, err)
		return nil, err
	}

	httpPort, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		log.Errorf("Error while getting TCP port for multiaddr %s: %s", portStr, err)
		return nil, err
	}

	url := fmt.Sprintf("http://%s:%v/api/v0/id", hostAddr, httpPort)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		log.Errorf("Error while getting %s: %s", url, err)
		return nil, err
	}
	defer resp.Body.Close()

	var idResp IdResponse
	err = json.NewDecoder(resp.Body).Decode(&idResp)
	if err != nil {
		log.Errorf("Error while decoding JSON response from %s: %s", url, err)
		return nil, err
	}

	port := ""

	for _, a := range idResp.Addresses {
		addr, err := ma.NewMultiaddr(a)

		if err != nil {
			log.Warnf("Error parsing multiaddr %s from peer %s", a, httpAddr)
			continue
		}
		port, err = addr.ValueForProtocol(ma.ProtocolWithName("udp").Code)
		log.Debugf("udp: %s, %s", port, err)
		if err != nil {
			port, err = addr.ValueForProtocol(ma.ProtocolWithName("tcp").Code)
			if err != nil {
				log.Warnf("Cannot find tcp/udp port in multiaddr %s from peer %s: %s", addr, httpAddr, err)
				continue
			}
			port = fmt.Sprintf("/tcp/%s", port)
			break
		}

		port = fmt.Sprintf("/udp/%s/quic", port)
		break
	}

	if len(port) == 0 {
		return nil, fmt.Errorf("cannot find tcp/udp port for peer %s, addresses: %v", httpAddr, idResp.Addresses)
	}

	addrStr := fmt.Sprintf("%s%s/p2p/%s", host, port, idResp.ID)
	addr, err := ma.NewMultiaddr(addrStr)
	if err != nil {
		return nil, fmt.Errorf("error while parsing multiaddr %s: %s", addrStr, err)
	}

	_, id := peer.SplitAddr(addr)

	peerInfo := &PeerInfo{
		ID:       id,
		Addr:     addr,
		IP:       hostAddr,
		HttpPort: uint16(httpPort),
	}

	log.Debugf("Downstream host %s is %s", httpAddr, peerInfo.Addr)
	return peerInfo, nil
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
func (d *Downstream) Next() peer.ID {
	// Do we need locking? Worst thing that can happen is 2 or more upstream peers connect at the
	// same time, they connect to the same downstream peer, and the next one gets skipped. I am
	// not sure that the performance penalty for locking is actually worth it.
	var peerId peer.ID

	// Save the current one in case no downstream hosts are available
	current := d.peerRing.Value.(ma.Multiaddr)
	log.Debugf("current: %s", current)

	for {
		addr := d.peerRing.Value.(ma.Multiaddr)
		d.peerRing = d.peerRing.Next()

		_, peerId = peer.SplitAddr(addr)
		log.Debugf("trying peer: %s", peerId)

		if d.host.Network().Connectedness(peerId) == network.Connected {
			break
		} else if addr == current {
			log.Warn("All downstream peers are disconnected. Waiting for a few seconds before retrying.")
			// TODO Make configurable, possibly backoff
			time.Sleep(5 * time.Second)
		}

	}
	return peerId
}

// Current returns the current downstream peer in the list
func (d *Downstream) Current() peer.ID {
	addr := d.peerRing.Value.(ma.Multiaddr)
	_, peerId := peer.SplitAddr(addr)
	return peerId
}

// Contains returns true if the given multiaddr is one of the downstream peers
func (d *Downstream) Contains(addr ma.Multiaddr) bool {
	_, peerId := peer.SplitAddr(addr)
	return d.ContainsPeer(peerId)
}

// ContainsPeer returns true if the given peer ID is one of the downstream peers
func (d *Downstream) ContainsPeer(id peer.ID) bool {
	_, found := d.peers[id]
	return found
}

// Peers returns a slice containing all the downstream peer IDs
func (d *Downstream) Peers() []peer.ID {
	peers := make([]peer.ID, 0, len(d.peers))
	for id := range d.peers {
		peers = append(peers, id)
	}
	return peers
}

// PeerAddrs returns a slice containing all the downstream peer bitswap multiaddrs
func (d *Downstream) PeerAddrs() []ma.Multiaddr {
	addrs := make([]ma.Multiaddr, 0, len(d.peers))
	for _, info := range d.peers {
		addrs = append(addrs, info.Addr)
	}
	return addrs
}

// ConnectAll connects to all the downstream peers and establishes a background goroutine
// to reconnect when they disconnect
func (d *Downstream) connectAll(n *Notifiee) {
	d.host.Network().Notify(n)

	d.peerRing.Do(func(target interface{}) {
		peerInfo, err := peer.AddrInfoFromP2pAddr(target.(ma.Multiaddr))
		if err != nil {
			log.Errorf("Error while parsing multiaddr %s: %s", target, err)
		} else {
			log.Debugf("Connecting to downstream peer %s", target)
			go d.connectLoop(*peerInfo)
		}
	})
}

// connectLoop is a goroutine that periodically tries to connect to a disconnected peer
func (d *Downstream) connectLoop(info peer.AddrInfo) {
	log.Debugf("Starting reconnection loop for downstream peer %s", info)
	for {
		err := d.host.Connect(d.ctx, info)
		if err == nil {
			break
		}

		log.Errorf("Failed to reconnect to downstream peer %v. Attempting reconnection. Error: %s", info, err)
		// TODO Make configurable, possibly backoff
		time.Sleep(5 * time.Second)
	}
}
