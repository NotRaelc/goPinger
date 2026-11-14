package main

import (
	"encoding/json"
	"errors"
	"sync"
	"time"
)

const (
	maxSubsPerUser = 5
	batchDelay     = 200 * time.Millisecond
)

var (
	ErrSSELimit   = errors.New("sse: global capacity exhausted")
	ErrSSEUserMax = errors.New("sse: too many connections for user")
)

type eventHub struct {
	mu      sync.Mutex
	subs    map[string]map[chan []byte]struct{}
	pending map[string][]ServerDTO
	timers  map[string]*time.Timer
	total   int
}

var hub = &eventHub{
	subs:    make(map[string]map[chan []byte]struct{}),
	pending: make(map[string][]ServerDTO),
	timers:  make(map[string]*time.Timer),
}

func subscribe(userID string) (chan []byte, error) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.total >= cfg.SSEMaxConnections {
		return nil, ErrSSELimit
	}
	set := hub.subs[userID]
	if len(set) >= maxSubsPerUser {
		return nil, ErrSSEUserMax
	}
	ch := make(chan []byte, 64)
	if set == nil {
		set = make(map[chan []byte]struct{})
		hub.subs[userID] = set
	}
	set[ch] = struct{}{}
	hub.total++
	return ch, nil
}

func unsubscribe(userID string, ch chan []byte) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if set, ok := hub.subs[userID]; ok {
		if _, exists := set[ch]; exists {
			delete(set, ch)
			hub.total--
		}
		if len(set) == 0 {
			delete(hub.subs, userID)
		}
	}
}

// publishState буферизует обновления и шлёт одним SSE-фреймом раз в 200мс.
func publishState(userID string, s Server, st ServerState) {
	dto := buildDTO(s, &st)
	hub.mu.Lock()
	hub.pending[userID] = append(hub.pending[userID], dto)
	if _, running := hub.timers[userID]; running {
		hub.mu.Unlock()
		return
	}
	hub.timers[userID] = time.AfterFunc(batchDelay, func() { flushBatch(userID) })
	hub.mu.Unlock()
}

func flushBatch(userID string) {
	hub.mu.Lock()
	dtoList := hub.pending[userID]
	delete(hub.pending, userID)
	delete(hub.timers, userID)
	subs := make([]chan []byte, 0, len(hub.subs[userID]))
	for ch := range hub.subs[userID] {
		subs = append(subs, ch)
	}
	hub.mu.Unlock()

	if len(dtoList) == 0 || len(subs) == 0 {
		return
	}
	b, err := json.Marshal(dtoList)
	if err != nil {
		return
	}
	for _, ch := range subs {
		select {
		case ch <- b:
		default:
		}
	}
}
