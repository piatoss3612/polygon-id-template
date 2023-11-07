package main

type Hub struct {
	clients    map[ID]*Client
	register   chan RegisterRequest
	unregister chan ID
	send       chan Message
}

type RegisterRequest struct {
	ID     ID
	Client *Client
}

func NewHub() *Hub {
	hub := &Hub{
		register:   make(chan RegisterRequest),
		unregister: make(chan ID),
		clients:    make(map[ID]*Client),
		send:       make(chan Message),
	}

	go hub.run()

	return hub
}

func (h *Hub) run() {
	for {
		select {
		case req := <-h.register:
			h.clients[req.ID] = req.Client
		case id := <-h.unregister:
			if client, ok := h.clients[id]; ok {
				close(client.send)
				delete(h.clients, id)
			}
		case msg := <-h.send:
			client, ok := h.clients[msg.ID]
			if ok {
				select {
				case client.send <- msg:
				default:
					close(client.send)
					delete(h.clients, msg.ID)
				}
			}

		}
	}
}
