package main

import (
	"encoding/json"
	"sync"
)

type eventHub struct {
	mu   sync.RWMutex
	subs map[string]map[chan []byte]struct{}
}

var hub = &eventHub{subs: make(map[string]map[chan []byte]struct{})}

func subscribe(userID string) chan []byte {
	ch := make(chan []byte, 16)
	hub.mu.Lock()
	if hub.subs[userID] == nil {
		hub.subs[userID] = make(map[chan []byte]struct{})
	}
	hub.subs[userID][ch] = struct{}{}
	hub.mu.Unlock()
	return ch
}

func unsubscribe(userID string, ch chan []byte) {
	hub.mu.Lock()
	if set, ok := hub.subs[userID]; ok {
		delete(set, ch)
		if len(set) == 0 {
			delete(hub.subs, userID)
		}
	}
	hub.mu.Unlock()
	// Не закрываем ch: может быть конкурентная отправка из publishState.
}

// publishState отправляет обновление ServerDTO всем SSE-подписчикам пользователя.
func publishState(userID string, s Server, st ServerState) {
	dto := buildDTO(s, &st)
	b, err := json.Marshal(dto)
	if err != nil {
		return
	}
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	for ch := range hub.subs[userID] {
		select {
		case ch <- b:
		default:
		}
	}
}
