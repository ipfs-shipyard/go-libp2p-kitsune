package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"

	logging "github.com/ipfs/go-log/v2"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/mcamou/go-libp2p-kitsune/bitswap"
	cm "github.com/mcamou/go-libp2p-kitsune/connection_manager"
	"github.com/mcamou/go-libp2p-kitsune/copy"
)

type DownstreamHost struct {
	ipfs_multiaddr ma.Multiaddr
	api_port       uint64
}

var log = logging.Logger("main")

func main() {
	// TODO get config from env vars / config file or both
	// TODO functional and performance tests
	// TODO Add metrics

	// Command-line options

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listenF := flag.String("l", "/ip4/0.0.0.0/tcp/0", "multiaddr to listen on for TCP connections (default: /ip4/0.0.0.0/tcp/0)")
	wsF := flag.String("w", "", "multiaddr to listen on for WebSocket connections (default: don't listen for websockets)")
	targetF := flag.String("d", "", "comma-separated list of downstream peer API port multiaddrs (mandatory)")
	keyFileF := flag.String("k", "./key", "File to read/store key (default: ./key)")
	preloadF := flag.Uint64("p", 0, "Enable preload node functionality and set preload API port")
	flag.Parse()

	if len(*targetF) == 0 {
		fmt.Println("You must specify the -d flag")
		os.Exit(1)
	}

	// Parse and validate options
	targetAddrsStr := strings.Split(*targetF, ",")
	targetAddrs := make([]ma.Multiaddr, 0, len(targetAddrsStr))
	for _, s := range targetAddrsStr {
		addr, err := ma.NewMultiaddr(s)
		if err != nil {
			fmt.Printf("Invalid multiaddr %s specified for -d flag: %s\n", s, err)
			os.Exit(1)
		}
		targetAddrs = append(targetAddrs, addr)
	}

	if *preloadF > 65535 {
		fmt.Printf("Invalid preload port %v, must be < 65536\n", *preloadF)
		os.Exit(1)
	}
	preloadEnabled := *preloadF != uint64(0)

	if preloadEnabled && *wsF == "" {
		fmt.Println("You must specify -w if you specify -p")
		os.Exit(1)
	}

	listenMaddr, err := ma.NewMultiaddr(*listenF)
	if err != nil {
		fmt.Printf("Invalid multiaddr %s specified for -l flag: %s\n", *listenF, err)
		os.Exit(1)
	}

	var wsAddr *ma.Multiaddr

	if *wsF != "" {
		if !strings.HasSuffix(*wsF, "/ws") {
			*wsF += "/ws"
		}
		addr, err := ma.NewMultiaddr(*wsF)
		if err != nil {
			fmt.Printf("Invalid multiaddr %s specified for -w flag: %s\n", *listenF, err)
			os.Exit(1)
		}
		wsAddr = &addr
	}

	// Initialize
	priv, err := getPrivateKey(*keyFileF)
	if err != nil {
		fmt.Printf("Error while getting private key from file %s: %v\n", *keyFileF, err)
		os.Exit(1)
	}

	h, err := makeHost(listenMaddr, wsAddr, priv)
	if err != nil {
		log.Fatalf("Error while making host: %v\n", err)
	}

	connMgr, err := cm.New(h, ctx, targetAddrs...)
	if err != nil {
		log.Fatalf("Error while creating connection manager: %v\n", err)
	}
	connMgr.ConnectAllDown()

	log.Infof("Peer ID: %s", h.ID())
	log.Info("Proxy addresses:")
	printAddrs(getHostAddresses(h), 4)
	log.Info("Downstream hosts:")
	printAddrs(connMgr.DownPeers(), 4)

	addHandlers(ctx, h, connMgr, preloadEnabled)

	if preloadEnabled {
		startPreload(connMgr, *preloadF)
	}
	log.Infof("Listening for bitswap connections on %s\n", h.Network().ListenAddresses()[0])

	// Run until canceled.
	<-ctx.Done()

	// TODO Send WANT cancels for all wanted CIDs to all downstream peers, and intercept SIGINT
}

func getPrivateKey(keyFile string) (crypto.PrivKey, error) {
	var priv crypto.PrivKey

	_, err := os.Stat(keyFile)
	if errors.Is(err, os.ErrNotExist) {
		r := rand.Reader

		priv, _, err = crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
		if err != nil {
			return nil, err
		}

		privEnc, err := crypto.MarshalPrivateKey(priv)
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(keyFile, privEnc, os.FileMode(int(0600)))
		if err != nil {
			return nil, err
		}
	} else {
		privEnc, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, err
		}

		priv, err = crypto.UnmarshalPrivateKey(privEnc)
		if err != nil {
			return nil, err
		}
	}

	return priv, err
}

func makeHost(listenAddr ma.Multiaddr, wsAddr *ma.Multiaddr, priv crypto.PrivKey) (host.Host, error) {
	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddr),
		libp2p.Identity(priv),
		libp2p.DisableRelay(),
		libp2p.ForceReachabilityPrivate(),
	}

	if wsAddr != nil {
		opts = append(opts, libp2p.ListenAddrs(*wsAddr))
	}

	ha, err := libp2p.New(opts...)

	return ha, err
}

func addHandlers(ctx context.Context, h host.Host, connMgr *cm.ConnectionManager, enablePreload bool) {
	// Protocols that we handle ourselves
	ping.NewPingService(h)

	// It would be nice to make this more generic
	bitswap.AddBitswapHandler(h, connMgr, enablePreload)

	// Generic handler for any other protocols
	h.SetStreamHandlerMatch("", copy.CopyMatcher, copy.CopyHandler(h, connMgr))
}

func getHostAddresses(h host.Host) []ma.Multiaddr {
	peerId, _ := ma.NewMultiaddr(fmt.Sprintf("/p2p/%s", h.ID().Pretty()))

	hostAddrs := h.Addrs()
	addrs := make([]ma.Multiaddr, 0, len(hostAddrs))
	for _, baseAddr := range hostAddrs {
		addrString := baseAddr.Encapsulate(peerId).String()
		addr, _ := ma.NewMultiaddr(addrString)
		addrs = append(addrs, addr)
	}
	return addrs
}

func printAddrs(addrs []ma.Multiaddr, indent int) {
	for _, elem := range addrs {
		log.Infof("%s%s", strings.Repeat(" ", indent), elem)
	}
}

func startPreload(connMgr *cm.ConnectionManager, port uint64) {
	portStr := fmt.Sprintf(":%v", port)
	log.Infof("Preload mode enabled with API port %v", port)
	http.HandleFunc("/api/v0/refs", preloadRefsHandler(connMgr))
	http.ListenAndServe(portStr, nil)
}

func preloadRefsHandler(connMgr *cm.ConnectionManager) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cid, found := r.URL.Query()["arg"]

		remoteIP := getRemoteIP(r)
		log.Debugf("IP %s requested %v", remoteIP, cid)

		// TODO Optimize downstream want handling
		// Figure out if we already have downstream node(s) assigned to this IP
		//   - This might be the UpstreamWantMap
		// Otherwise assign a new downstream node to this IP
		// Forward the /api/v0/refs call to the downstream node. With the reply:
		//   - Forward to the upstream node
		//   - Parse it and add the CIDs to the UpstreamWantMap - it will have to map
		//     downstreamPeer <-> upstreamIP, and then in bitswap we need to figure out
		//     the peerID

		w.Header().Set("Access-Control-Allow-Origin", "*")

		if found {
			// We don't know which downstream peer this peer is associated with, so just
			// grab the current one (perhaps a random one would be better?)
			peerId := connMgr.GetCurrentDownPeer()

			peerInfo, found := connMgr.GetDownPeerInfo(peerId)
			if !found {
				log.Errorf("Peer %s not found in downstream peers, not sending refs request", peerId)
				return
			}

			url := fmt.Sprintf("http://%s:%v/api/v0/refs?recursive=true&arg=%s", peerInfo.IP, peerInfo.HttpPort, cid[0])
			log.Debugf("Fetching %s", url)

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
