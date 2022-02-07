package main

// Adapted from https://gist.github.com/hartfordfive/77f2e6de9abd3be4ba66123b9e526f0f

import (
	"container/ring"
	"fmt"

	"github.com/libp2p/go-libp2p-core/peer"
)

type PeerList struct {
	list  *ring.Ring
	peers map[peer.ID]bool
}

func NewPeerList(items ...peer.ID) *PeerList {
	peers := make(map[peer.ID]bool)
	l := &PeerList{
		list:  ring.New(len(items)),
		peers: peers,
	}
	for i := 0; i < l.list.Len(); i++ {
		l.list.Value = items[i]
		l.list = l.list.Next()

		peers[items[i]] = true
	}
	return l
}

func (l *PeerList) String() string {
	s := ""
	l.list.Do(func(elem interface{}) {
		s += elem.(fmt.Stringer).String()
		s += " "
	})
	return s
}

func (l *PeerList) GetItem() peer.ID {
	val := l.list.Value
	l.list = l.list.Next()
	return val.(peer.ID)
}

func (l *PeerList) Contains(id peer.ID) bool {
	enabled, found := l.peers[id]
	return enabled && found
}
