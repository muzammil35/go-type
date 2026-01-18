package main

import (
    "log"
    "net/http"
	cl "go-type/internal/client"
    srv "go-type/internal/server" // adjust import path
)

func main() {
    // Initialize server
    server := &srv.Server{
        Clients:          make(map[string]*cl.Client),
        Broadcast:        make(chan []byte),
        MatchmakingChan:  make(chan srv.MatchmakingAction, 100),
    }

    // Start matchmaking manager
    go srv.MatchmakingManager(server.MatchmakingChan)

    // Register WebSocket endpoint
    http.HandleFunc("/ws", server.HandleConnect)

    addr := ":8080"
    log.Printf("Server started on %s", addr)

    if err := http.ListenAndServe(addr, nil); err != nil {
        log.Fatal(err)
    }
}