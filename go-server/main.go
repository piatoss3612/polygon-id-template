package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/iden3/go-circuits/v2"
	auth "github.com/iden3/go-iden3-auth/v2"
	"github.com/iden3/go-iden3-auth/v2/loaders"
	"github.com/iden3/go-iden3-auth/v2/pubsignals"
	"github.com/iden3/go-iden3-auth/v2/state"
	"github.com/iden3/iden3comm/v2/protocol"
	_ "github.com/joho/godotenv/autoload"

	socketio "github.com/googollee/go-socket.io"
	"github.com/googollee/go-socket.io/engineio"
	"github.com/googollee/go-socket.io/engineio/transport"
	"github.com/googollee/go-socket.io/engineio/transport/polling"
	"github.com/googollee/go-socket.io/engineio/transport/websocket"
)

var allowOriginFunc = func(r *http.Request) bool {
	return true
}

func main() {
	io := socketio.NewServer(&engineio.Options{
		Transports: []transport.Transport{
			&polling.Transport{
				CheckOrigin: allowOriginFunc,
			},
			&websocket.Transport{
				HandshakeTimeout: 10 * time.Second,
				CheckOrigin:      allowOriginFunc,
			},
		},
	})

	io.OnConnect("/", func(c socketio.Conn) error {
		c.SetContext("")
		fmt.Println("connected:", c.ID())
		return nil
	})

	go func() {
		if err := io.Serve(); err != nil {
			log.Fatal(err)
		}
	}()

	mux := chi.NewRouter()

	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)

	mux.Handle("/socket.io/", io)
	mux.Get("/api/get-auth-qr", GetAuthRequest)
	mux.Post("/api/verification-callback", Callback)

	srv := &http.Server{Addr: ":8080", Handler: mux}

	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}

var requestMap = make(map[string]interface{})

func GetAuthRequest(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	sessionId := r.URL.Query().Get("sessionId")

	log.Println("Session ID: ", sessionId)

	uri := fmt.Sprintf("%s/api/verification-callback?sessionId=%s",
		os.Getenv("HOSTED_SERVER_URL"),
		sessionId,
	)

	audience := "did:polygonid:polygon:mumbai:2qDyy1kEo2AYcP3RT4XGea7BtxsY285szg6yP9SPrs"

	var request protocol.AuthorizationRequestMessage = auth.CreateAuthorizationRequest(
		"",
		audience,
		uri,
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

	fmt.Println(request)

	msgBytes, _ := json.Marshal(request)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msgBytes)
}

func Callback(w http.ResponseWriter, r *http.Request) {
	sessionId := r.URL.Query().Get("sessionId")

	tokenBytes, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	authRequest, ok := requestMap[sessionId]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ipfsURL := "https://ipfs.io"
	contractAddress := "0x134B1BE34911E39A8397ec6289782989729807a4"
	resolverPrefix := "polygon:mumbai"
	keyDIR := "./keys"

	var verificationKeyLoader = &loaders.FSKeyLoader{
		Dir: keyDIR,
	}

	resolver := state.ETHResolver{
		RPCUrl:          os.Getenv("PRC_URL_MUMBAI"),
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

	userID := authResponse.From

	messageBytes := []byte("User with ID " + userID + " Successfully authenticated")

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(messageBytes)
}
