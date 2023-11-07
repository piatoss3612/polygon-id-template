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
	"github.com/go-chi/cors"

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

	conn, ok := ClientMap[ID(sessionId)]
	if !ok || conn == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	go func() {
		err := conn.WriteJSON(Message{
			Type: EventMessage,
			Event: Event{
				Fn:     "getAuthQr",
				Status: InProgress,
				Data:   sessionId,
			},
		})
		if err != nil {
			log.Println(err)
		}
	}()

	uri := fmt.Sprintf("%s/api/verification-callback?sessionId=%s",
		os.Getenv("HOSTED_SERVER_URL"),
		sessionId,
	)

	audience := "did:polygonid:polygon:mumbai:2qDyy1kEo2AYcP3RT4XGea7BtxsY285szg6yP9SPrs"

	var request protocol.AuthorizationRequestMessage = auth.CreateAuthorizationRequest(
		"Must be born before this year",
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

	go func() {
		err := conn.WriteJSON(Message{
			Type: EventMessage,
			Event: Event{
				Fn:     "getAuthQr",
				Status: Done,
				Data:   request,
			},
		})
		if err != nil {
			log.Println(err)
		}
	}()

	msgBytes, _ := json.Marshal(request)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(msgBytes)
}

func Callback(w http.ResponseWriter, r *http.Request) {
	sessionId := r.URL.Query().Get("sessionId")

	conn, ok := ClientMap[ID(sessionId)]
	if !ok || conn == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	tokenBytes, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	authRequest, ok := requestMap[sessionId]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	go func() {
		err := conn.WriteJSON(Message{
			Type: EventMessage,
			Event: Event{
				Fn:     "handleVerification",
				Status: InProgress,
				Data:   authRequest,
			},
		})
		if err != nil {
			log.Println(err)
		}
	}()

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
		log.Println("here1234", err)
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
		log.Println("here12345678", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	go func() {
		err = conn.WriteJSON(Message{
			Type: EventMessage,
			Event: Event{
				Fn:     "handleVerification",
				Status: Done,
				Data:   authResponse,
			},
		})
		if err != nil {
			log.Println(err)
		}
	}()

	userID := authResponse.From

	messageBytes := []byte("User with ID " + userID + " Successfully authenticated")

	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/json")
	w.Write(messageBytes)
}
