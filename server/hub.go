package main

import "sync"

type Hub struct {
	clients    map[ID]*Client
	register   chan *Client
	unregister chan ID
	send       chan Message

	mu *sync.RWMutex
}

func NewHub() *Hub {
	hub := &Hub{
		register:   make(chan *Client),
		unregister: make(chan ID),
		clients:    make(map[ID]*Client),
		send:       make(chan Message),
		mu:         &sync.RWMutex{},
	}

	go hub.run()

	return hub
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			// register client
			if client == nil {
				continue
			}

			id := client.id

			h.mu.Lock()
			h.clients[id] = client
			h.mu.Unlock()
		case id := <-h.unregister:
			// unregister client if client is registered
			h.mu.Lock()
			client, ok := h.clients[id]
			if ok {
				close(client.send)
				delete(h.clients, id)
			}
			h.mu.Unlock()
		case msg := <-h.send:
			// send message to client if client is registered
			h.mu.RLock()
			client, ok := h.clients[msg.ID]
			h.mu.RUnlock()

			if ok {
				select {
				case client.send <- msg:
				default:
					h.mu.Lock()
					close(client.send)
					delete(h.clients, msg.ID)
					h.mu.Unlock()
				}
			}

		}
	}
}
