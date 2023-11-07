package main

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type MessageType string

const (
	IDMessage    MessageType = "id"
	EventMessage MessageType = "event"
)

type ID string

type Message struct {
	Type  MessageType `json:"type"`
	ID    ID          `json:"id,omitempty"`
	Event Event       `json:"event,omitempty"`
}

type Status string

const (
	InProgress Status = "IN_PROGRESS"
	Error      Status = "ERROR"
	Done       Status = "DONE"
)

type Event struct {
	Fn     string `json:"fn"`
	Status Status `json:"status"`
	Data   any    `json:"data"`
}

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512
)

var ClientMap map[ID]*websocket.Conn = make(map[ID]*websocket.Conn)

var allowOriginFunc = func(r *http.Request) bool {
	return true
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     allowOriginFunc,
}

func ServeWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Could not open websocket connection", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()

	msg := Message{
		Type: IDMessage,
		ID:   ID(id),
	}

	b, err := json.Marshal(msg)
	if err != nil {
		http.Error(w, "Could not marshal message", http.StatusInternalServerError)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, b)
	if err != nil {
		http.Error(w, "Could not write message", http.StatusInternalServerError)
		return
	}

	ClientMap[ID(id)] = conn

	// go func() {}()
}
