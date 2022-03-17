package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p-core/peer"
	cm "github.com/mcamou/go-libp2p-kitsune/connection_manager"
)

func startPreloadHandler(connMgr *cm.ConnectionManager, port uint64) {
	portStr := fmt.Sprintf(":%v", port)
	log.Infof("Preload mode enabled with API port %v", port)
	http.HandleFunc("/api/v0/refs", preloadRefsHandler(connMgr))
	err := http.ListenAndServe(portStr, nil)
	if err != nil {
		log.Fatalf("Error starting HTTP listener")
	}
}

func preloadRefsHandler(connMgr *cm.ConnectionManager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cStr, found := r.URL.Query()["arg"]

		remoteIP := getRemoteIP(r)

		if found {
			c, err := cid.Parse(cStr[0])
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				log.Debugf("IP %s requested bad %v", remoteIP, c)
				return
			}
			log.Debugf("IP %s requested %v", remoteIP, c)

			w.Header().Set("Access-Control-Allow-Origin", "*")

			var peerId peer.ID
			upPeers := connMgr.UpstreamPeersForIP(remoteIP)
			if len(upPeers) > 0 {
				// We already have some peers for this IP - send the request to the same downstream
				// peer. The main issue with this is that if the upstream peer is behind a NAT, all
				// upstream peers behind that same IP will be assigned to the same downstream peer.
				peerId = connMgr.DownstreamForPeer(upPeers[0])[0]
			} else {
				// This peer's IP is not associated with any upstream peer, so just grab the last one
				// we used (perhaps a random one would be better?). This should only happen when
				// js-ipfs starts up and sends a `refs` request before libp2p is fully connected,
				// or if the libp2p channel disconnects.
				peerId = connMgr.CurrentDownPeer()
			}

			peerInfo, found := connMgr.DownPeerInfo(peerId)
			if !found {
				log.Errorf("Peer %s not found in downstream peers, not sending refs request", peerId)
				return
			}

			url := fmt.Sprintf("http://%s:%v/api/v0/refs?recursive=true&arg=%s", peerInfo.IP, peerInfo.HttpPort, c)
			log.Debugf("Fetching %s", url)
			connMgr.AddRefCid(remoteIP, c)

			// TODO Stream response
			resp, err := http.Post(url, "application/json", nil)
			if err != nil {
				log.Errorf("HTTP error while fetching %s", err)
				return
			}

			io.Copy(w, resp.Body)
		}
	}
}

func getRemoteIP(r *http.Request) net.IP {
	var remoteIP string
	if len(r.Header.Get("X-Forwarded-For")) > 1 {
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/X-Forwarded-For
		remoteIP = r.Header.Get("X-Forwarded-For")
		if strings.Contains(remoteIP, ",") {
			remoteIP = strings.Split(remoteIP, ",")[0]
		}
	} else {
		if strings.Contains(r.RemoteAddr, ":") {
			remoteIP = strings.Split(r.RemoteAddr, ":")[0]
		} else {
			remoteIP = r.RemoteAddr
		}
	}

	return net.ParseIP(remoteIP)
}
