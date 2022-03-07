package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"io"
	mrand "math/rand"
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

	listenF := flag.String("l", "/ip4/0.0.0.0/tcp/0", "multiaddr to listen on (default: listen on all IPs on a random port)")
	targetF := flag.String("d", "", "comma-separated list of downstream peer API port multiaddrs (mandatory)")
	seedF := flag.Int64("s", 0, "set random seed for id generation. 0 means use a random number (default: 0)")
	keyFileF := flag.String("k", "./key", "File to read/store key (default: ./key)")
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

	listenMaddr, err := ma.NewMultiaddr(*listenF)
	if err != nil {
		fmt.Printf("Invalid multiaddr %s specified for -l flag: %s\n", *listenF, err)
		os.Exit(1)
	}

	// Initialize
	priv, err := getPrivateKey(*seedF, *keyFileF)
	if err != nil {
		fmt.Printf("Error while getting private key from file %s: %v\n", *keyFileF, err)
		os.Exit(1)
	}

	h, err := makeHost(listenMaddr, priv)
	if err != nil {
		log.Fatalf("Error while making host: %v\n", err)
	}

	log.Info("Proxy addresses:")
	printAddrs(getHostAddresses(h), 4)

	log.Info("Downstream hosts:")
	printAddrs(targetAddrs, 4)

	connMgr, err := cm.New(h, ctx, targetAddrs...)
	if err != nil {
		log.Fatalf("Error while creating connection manager: %v\n", err)
	}
	connMgr.ConnectAllDown()

	addHandlers(ctx, h, connMgr, *listenF)

	log.Infof("Listening for connections on %s\n", h.Network().ListenAddresses()[0])

	// Run until canceled.
	<-ctx.Done()
}

func getPrivateKey(randseed int64, keyFile string) (crypto.PrivKey, error) {
	var priv crypto.PrivKey

	_, err := os.Stat(keyFile)
	if errors.Is(err, os.ErrNotExist) {
		var r io.Reader
		if randseed == 0 {
			r = rand.Reader
		} else {
			r = mrand.New(mrand.NewSource(randseed))
		}

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

func makeHost(listenMaddr ma.Multiaddr, priv crypto.PrivKey) (host.Host, error) {
	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenMaddr),
		libp2p.Identity(priv),
		libp2p.DisableRelay(),
		libp2p.ForceReachabilityPrivate(),
	}

	ha, err := libp2p.New(opts...)

	return ha, err
}

func addHandlers(ctx context.Context, h host.Host, connMgr *cm.ConnectionManager, listenMultiaddr string) {
	// Protocols that we handle ourselves
	ping.NewPingService(h)

	// It would be nice to make this more generic
	bitswap.AddBitswapHandler(h, connMgr)

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
