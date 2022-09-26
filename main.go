package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"

	logging "github.com/ipfs/go-log/v2"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/ipfs-shipyard/go-libp2p-kitsune/bitswap"
	"github.com/ipfs-shipyard/go-libp2p-kitsune/copy"
	pmgr "github.com/ipfs-shipyard/go-libp2p-kitsune/peer_manager"
	"github.com/ipfs-shipyard/go-libp2p-kitsune/prometheus"
)

var log = logging.Logger("main")

func main() {
	// TODO Get config from env vars / config file or both (see e.g. https://github.com/spf13/viper)
	// TODO Performance/load tests

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

	peerMgr, err := pmgr.New(h, ctx, targetAddrs...)
	if err != nil {
		log.Fatalf("Error while creating connection manager: %v\n", err)
	}
	peerMgr.ConnectAllDown()

	log.Infof("Peer ID: %s", h.ID())
	log.Info("Proxy addresses:")
	printAddrs(getHostAddresses(h), 4)
	log.Info("Downstream peers:")
	printAddrs(peerMgr.DownPeers(), 4)

	addProtoHandlers(ctx, h, peerMgr, preloadEnabled)

	if preloadEnabled {
		startPreloadHandler(peerMgr, *preloadF)
	}
	log.Debugf("Listening for bitswap connections on %s\n", h.Network().ListenAddresses()[0])

	prometheus.StartPrometheus(9090)

	// Run until canceled.
	<-ctx.Done()
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
	// TODO The PeerID is currently encoded as CIDv0. This is OK for the preloads but not for the
	//      general use case of replacing any go-ipfs node.
	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddr),
		libp2p.Identity(priv),
		libp2p.DisableRelay(),
		libp2p.ForceReachabilityPrivate(),
	}

	if wsAddr != nil {
		opts = append(opts, libp2p.ListenAddrs(*wsAddr))
	}

	return libp2p.New(opts...)
}

func addProtoHandlers(ctx context.Context, h host.Host, peerMgr *pmgr.PeerManager, enablePreload bool) {
	// Protocols that we handle ourselves
	ping.NewPingService(h)

	// It would be nice to make this more generic
	bs := bitswap.New(h, peerMgr, enablePreload)
	bs.AddHandler()

	// Generic handler for any other protocols
	h.SetStreamHandlerMatch("", copy.Matcher, copy.Handler(h, peerMgr))
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
