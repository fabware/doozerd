package paxos

import (
	"junta/store"
	"container/heap"
	"container/vector"
	"math"
	"sort"
)

type registrar struct {
	self     string
	window   int
	evs      chan store.Event
	lookupCh chan lookup
	lookups  *lookupQueue
}

type lookup struct {
	cver uint64
	ch   chan *cluster
}

func (l lookup) Less(y interface{}) bool {
	return l.cver < y.(lookup).cver
}

type lookupQueue struct {
	vector.Vector
}

func (q *lookupQueue) peek() lookup {
	if q.Len() == 0 {
		return lookup{cver: math.MaxUint64} // ~infinity
	}
	return q.At(0).(lookup)
}

// This thing keeps track of who is supposed to be in the cluster for every
// seqn. It also remembers the network address of every member.
func newRegistrar(self string, st *store.Store, window int) *registrar {
	rg := &registrar{
		self:     self,
		window:   window,
		evs:      make(chan store.Event),
		lookupCh: make(chan lookup),
		lookups:  new(lookupQueue),
	}
	heap.Init(rg.lookups)
	st.Watch("/b/junta/members", store.Add|store.Rem, rg.evs)
	st.WatchApply(rg.evs)
	go rg.process()
	return rg
}

func findString(v []string, s string) (i int) {
	for i = range v {
		if v[i] == s {
			return
		}
	}
	return -1
}

func (rg *registrar) process() {
	known := uint64(0)
	members := vector.StringVector{}
	clusters := make(map[uint64]*cluster)

	for {
		select {
		case l := <-rg.lookupCh:
			heap.Push(rg.lookups, l)
		case ev := <-rg.evs:
			switch {
			case ev.Path == "/b/junta/members":
				switch ev.Type {
				case store.Add:
					members.Push(ev.Body)
				case store.Rem:
					members.Delete(findString(members, ev.Body))
				}
			case ev.Type == store.Apply:
				known++
				sort.SortStrings(members)
				clusters[known] = newCluster(rg.self, members)
			}
		}

		// If we have any lookups that can be satisfied, do them.
		for l := rg.lookups.peek(); known >= l.cver; l = rg.lookups.peek() {
			l := heap.Pop(rg.lookups).(lookup)
			l.ch <- clusters[l.cver]
		}
	}
}

func (rg *registrar) clusterFor(seqn uint64) *cluster {
	cver := uint64(1)
	if seqn > uint64(rg.window) {
		cver = seqn - uint64(rg.window)
	}
	ch := make(chan *cluster)
	rg.lookupCh <- lookup{cver, ch}
	return <-ch
}
