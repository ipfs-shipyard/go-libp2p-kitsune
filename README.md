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

## Project status

Kitsune is under development. At this point it has successfully connected 2 go-ipfs
instances and transferred files between them using Bitswap.
