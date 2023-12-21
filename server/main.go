package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/iden3/go-circuits/v2"
	auth "github.com/iden3/go-iden3-auth/v2"
	"github.com/iden3/go-iden3-auth/v2/loaders"
	"github.com/iden3/go-iden3-auth/v2/pubsignals"
	"github.com/iden3/go-iden3-auth/v2/state"
	"github.com/iden3/iden3comm/v2/protocol"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	mux := chi.NewRouter()

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(cors.AllowAll().Handler)

	mux.HandleFunc("/ws", ServeWs)
	mux.Get("/api/get-login-qr", GetLoginRequest)
	mux.Get("/api/get-auth-qr", GetAuthRequest)
	mux.Post("/api/login-callback", LoginCallback)
	mux.Post("/api/verification-callback", VerificationCallback)

	srv := &http.Server{Addr: ":8080", Handler: mux}

	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}

var (
	hub        = NewHub()
	requestMap = make(map[string]interface{})
)

func GetLoginRequest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	sessionId := r.URL.Query().Get("sessionId")

	_, err := uuid.Parse(sessionId)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "getLoginQr",
			Status: InProgress,
			Data:   sessionId,
		},
	}

	callback := fmt.Sprintf("%s/api/login-callback?sessionId=%s",
		os.Getenv("HOSTED_SERVER_URL"),
		sessionId,
	)

	sender := "did:polygonid:polygon:mumbai:2qDyy1kEo2AYcP3RT4XGea7BtxsY285szg6yP9SPrs"

	var request protocol.AuthorizationRequestMessage = auth.CreateAuthorizationRequestWithMessage(
		"Login to Polygon",
		"Your Polygon ID",
		sender,
		callback,
	)

	request.ID = sessionId
	request.ThreadID = sessionId

	requestMap[sessionId] = request

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "getLoginQr",
			Status: Done,
			Data:   request,
		},
	}

	msgBytes, _ := json.Marshal(request)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msgBytes)
}

func LoginCallback(w http.ResponseWriter, r *http.Request) {
	sessionId := r.URL.Query().Get("sessionId")

	_, err := uuid.Parse(sessionId)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	tokenBytes, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	authRequest, ok := requestMap[sessionId]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "handleLogin",
			Status: InProgress,
			Data:   authRequest,
		},
	}

	ipfsURL := "https://ipfs.io"
	contractAddress := "0x134B1BE34911E39A8397ec6289782989729807a4"
	resolverPrefix := "polygon:mumbai"
	keyDIR := "./keys"

	var verificationKeyLoader = &loaders.FSKeyLoader{
		Dir: keyDIR,
	}

	resolver := state.ETHResolver{
		RPCUrl:          os.Getenv("RPC_URL_MUMBAI"),
		ContractAddress: common.HexToAddress(contractAddress),
	}

	resolvers := map[string]pubsignals.StateResolver{
		resolverPrefix: &resolver,
	}

	verifier, err := auth.NewVerifier(
		verificationKeyLoader,
		resolvers,
		auth.WithIPFSGateway(ipfsURL),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	authResponse, err := verifier.FullVerify(
		r.Context(),
		string(tokenBytes),
		authRequest.(protocol.AuthorizationRequestMessage),
		pubsignals.WithAcceptedStateTransitionDelay(time.Minute*5),
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "handleLogin",
			Status: Done,
			Data:   authResponse,
		},
	}

	userID := authResponse.From

	messageBytes := []byte("User with ID " + userID + " Successfully authenticated")

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(messageBytes)
}

func GetAuthRequest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	sessionId := r.URL.Query().Get("sessionId")

	_, err := uuid.Parse(sessionId)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "getAuthQr",
			Status: InProgress,
			Data:   sessionId,
		},
	}

	callback := fmt.Sprintf("%s/api/verification-callback?sessionId=%s",
		os.Getenv("HOSTED_SERVER_URL"),
		sessionId,
	)

	sender := "did:polygonid:polygon:mumbai:2qDyy1kEo2AYcP3RT4XGea7BtxsY285szg6yP9SPrs"

	var request protocol.AuthorizationRequestMessage = auth.CreateAuthorizationRequest(
		"Must be born before this year",
		sender,
		callback,
	)

	request.ID = sessionId
	request.ThreadID = sessionId

	var mtpProofRequest protocol.ZeroKnowledgeProofRequest
	mtpProofRequest.ID = 1
	mtpProofRequest.CircuitID = string(circuits.AtomicQuerySigV2CircuitID)
	mtpProofRequest.Query = map[string]interface{}{
		"allowedIssuers": []string{"*"},
		"credentialSubject": map[string]interface{}{
			"birthday": map[string]interface{}{
				"$lt": 20000101,
			},
		},
		"context": "https://raw.githubusercontent.com/iden3/claim-schema-vocab/main/schemas/json-ld/kyc-v3.json-ld",
		"type":    "KYCAgeCredential",
	}

	request.Body.Scope = append(request.Body.Scope, mtpProofRequest)

	requestMap[sessionId] = request

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "getAuthQr",
			Status: Done,
			Data:   request,
		},
	}

	msgBytes, _ := json.Marshal(request)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msgBytes)
}

func VerificationCallback(w http.ResponseWriter, r *http.Request) {
	sessionId := r.URL.Query().Get("sessionId")

	_, err := uuid.Parse(sessionId)
	if err != nil {
		http.Error(w, "invalid session id", http.StatusBadRequest)
		return
	}

	tokenBytes, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	authRequest, ok := requestMap[sessionId]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "handleVerification",
			Status: InProgress,
			Data:   authRequest,
		},
	}

	ipfsURL := "https://ipfs.io"
	contractAddress := "0x134B1BE34911E39A8397ec6289782989729807a4"
	resolverPrefix := "polygon:mumbai"
	keyDIR := "./keys"

	var verificationKeyLoader = &loaders.FSKeyLoader{
		Dir: keyDIR,
	}

	resolver := state.ETHResolver{
		RPCUrl:          os.Getenv("RPC_URL_MUMBAI"),
		ContractAddress: common.HexToAddress(contractAddress),
	}

	resolvers := map[string]pubsignals.StateResolver{
		resolverPrefix: &resolver,
	}

	verifier, err := auth.NewVerifier(
		verificationKeyLoader,
		resolvers,
		auth.WithIPFSGateway(ipfsURL),
	)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	authResponse, err := verifier.FullVerify(
		r.Context(),
		string(tokenBytes),
		authRequest.(protocol.AuthorizationRequestMessage),
		pubsignals.WithAcceptedStateTransitionDelay(time.Minute*5),
	)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	hub.send <- Message{
		Type: EventMessage,
		ID:   ID(sessionId),
		Event: Event{
			Fn:     "handleVerification",
			Status: Done,
			Data:   authResponse,
		},
	}

	userID := authResponse.From

	messageBytes := []byte("User with ID " + userID + " Successfully authenticated")

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(messageBytes)
}

var (
	allowOriginFunc = func(r *http.Request) bool {
		return true
	}

	upgrader = websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second,
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		CheckOrigin:      allowOriginFunc,
	}
)

func ServeWs(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Could not open websocket connection", http.StatusBadRequest)
		return
	}

	id := uuid.New().String()

	client := &Client{
		hub:  hub,
		id:   ID(id),
		conn: conn,
		send: make(chan Message, 256),
		mu:   &sync.Mutex{},
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()

	msg := Message{
		Type: IDMessage,
		ID:   ID(id),
	}

	client.send <- msg
}
