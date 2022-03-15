# Kitsune: a go-libp2p proxy/load balancer

Kitsune is a proxy/load balancer based on libp2p. It is currently centered on
load-balancing IPFS traffic.

## Why?

[Working document](https://www.notion.so/pl-strflt/Kitsune-a-libp2p-reverse-proxy-60df1d1a333646768951c2976e735234)

[libp2p](https://github.com/libp2p/libp2p) is a great peer-to-peer network stack.
It has awesome features such as built-in encryption, firewall hole-punching and
network protocol transparency. However, some of these features come with their
own costs.

Specifically, one problem we have experienced at [Protocol Labs](https://protocol.ai)
is that it is very difficult or impossible to horizontally scale a node,
especially when clients have hardcoded Multiaddresses to point to specific peers.
One specific case is the [js-ipfs](https://github.com/ipfs/js-ipfs) preload nodes.
The preload node multiaddresses are hardcoded in the js-ipfs library itself. While
we have added preload nodes as load increased, there are applications out there
that are still running with outdated versions of the library which know only of a
subset of them. This has resulted in increasing load on the first preload nodes
that were installed, and the only way to reduce the load is by scaling up. In the
long term, this is unsustainable.

The idea behind Kitsune is to replace the preload nodes with a load balancer,
which will then balance the load between downstream go-ipfs nodes. The load balancer
will take over the identity of the current preload nodes, allowing us to scale them
out.

## Project goals

The initial project goal is to be able to replace the preload nodes with Kitsune
instances, each backed by multiple go-ipfs nodes. This means that:

* Not all protocols that IPFS uses will be initially implemented
* Those protocols which are implemented will only be partially implemented, with
  enough functionality to work with the preload nodes.

Longer-term I hope to be able to more fully implement the protocols used by IPFS.

A side goal is to serve as a basis for creating debugging and monitoring tools.

## Additional uses

### p2p-p2p bridge

Kitsune can also be used to bridge between IPFS swarms, allowing for one-way traffic
without polluting either swarm's DHT. For example, by implementing support for
private upstream swarms (currently unimplemented), you could deploy an instance of
Kitsune backed by one or more nodes in the public swarm and peer it with all of the
hosts in the private swarm (since it does not participate in the DHT). This would
enable the hosts in the private swarm to get data from the public swarm, but not
vice versa.

## Project status

Kitsune is under development. At this point it has successfully connected 2 go-ipfs
instances and transferred files between them using Bitswap, as well as getting a
file from a different go-ipfs node by proxying via the downstream node.

## Compiling

Just run `go build`. You will need Go 1.17 (this is tested on 1.17.8). A `.tool-versions`
file is included if you use `asdf`.

## Running

The binary is called `go-libp2p-kitsune` and accepts the following flags:

`-d` (mandatory) Comma-separated list of downstream go-ipfs node API port multiaddrs
     (e.g. `/ip4/10.0.1.42/tcp/5002,/ip4/10.0.5.3/tcp/5002`). The backing go-ipfs
     nodes must allow access to the `/api/v0/id` and `/api/v0/refs` GRPC endpoints.

`-l` (optional) Multiaddr to listen on for TCP/UDP traffic (e.g. `/ip4/127.0.0.1/tcp/4001`).
     IP `0.0.0.0` means listening on all available IP addresses. The default is `/ip4/0.0.0.0/tcp/0`
     (all IP addresses, random port).

`-w` (optional in normal mode, mandatory in preload mode) Multiaddr to listen
     on for WebSocket traffic, with or without the `/ws` protocol (e.g. `/ip4/127.0.0.1/tcp/8080`
     or `/ip4/127.0.0.1/tcp/8080/ws`). IP `0.0.0.0` means listening on all available
     IP addresses. The default is `/ip4/0.0.0.0/tcp/0` (all IP addresses, random
     port).

`-k` (optional) Name of the keyfile. When Kitsune starts up the first time it will
     store its private key in this file. The next times it will read this file
     to get its identity/private key.

`-p` (optional) Enable preload mode and indicate the preload API port (see below)

## Preload mode (WIP)

Normally Kitsune will forward bitswap traffic in one direction only:

* WANTs go from upstream nodes to downstream nodes
* BLOCKs go from downstream nodes to upstream nodes

This is enough to e.g. connect 2 go-ipfs nodes (or swarms) and allow the upstream
swarm to get blocks from the downstream swarm and not vice versa, without adding
the upstream host to the downstream swarm's DHT.

Enabling the preload functionality allows Kitsune to act as a proxy for the js-ipfs
preload nodes. Apart from the basic functionality of allowing upstream peers to get
bitswap data from the downstream peers, it will:

* Enable the `/api/v0/refs` GRPC endpoint. The preloads use this for 2 things:

  * Pre-cache a whole IPLD graph and allow js-ipfs node to get all the associated
    blocks in parallel
  * When adding a file to IPFS, js-ipfs will call `/api/v0/refs?recursive=true&arg=<CID>`.
    This starts a bitswap session from the preload node to the js-ipfs node, so
    that the data is preserved in case e.g. of a browser reload

This second case means that we need to enable a limited version of Bitswap going
the inverse way: WANTs from downstream nodes and BLOCKs to upstream nodes. Calling
`/api/v0/refs?recursive=true&arg=<CID>` will enable this transfer for the specific
CID given and its child CIDs.


