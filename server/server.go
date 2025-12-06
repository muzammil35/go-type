package Server

import (
	"log"
	"net/http"
	"sync"
	"github.com/gorilla/websocket"
	cl "go-type/client" 
    "time"
    "encoding/json"
    "fmt"
    "os"
    "strings"
)

var waitRoom []*cl.Client

// Upgrader configures the WebSocket upgrade
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    // CheckOrigin allows connections from any origin (adjust for production)
    CheckOrigin: func(r *http.Request) bool {
        return true
    },
}

type Server struct {
	clients map[string] *cl.Client
	broadcast chan []byte
    mu        sync.Mutex
    matchmakingChan chan MatchmakingAction
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
    if err != nil {
        log.Printf("Failed to upgrade connection: %v", err)
        return
    }
    defer conn.Close()

    name := r.URL.Query().Get("name")

	c := cl.NewClient(name, cl.GenerateUserID(), conn)
	
	// Send add action to matchmaking channel
	s.matchmakingChan <- MatchmakingAction{Action: "add", ID: c.Id, Client: c}

    for {
        _, message, err := conn.ReadMessage()
        if err != nil {
            // Client disconnected or error occurred
            s.matchmakingChan <- MatchmakingAction{Action: "remove", ID: c.Id}
            break
        }
        for _, value := range s.clients {
            //if there is a client with an opponent, in a running game:
                //broadcast WPM to opponents
            //else:
                //return message to frontend that says "waiting for players"
        }
    }
    
    
}


// Create a channel for matchmaking requests
type MatchmakingAction struct {
    Action string // "add", "remove", "match"
    ID     string
    Client *cl.Client
}

// This replaces your Matchmaking function - runs as a single goroutine
func MatchmakingManager(actions <-chan MatchmakingAction) {
    clients := make(map[string]*cl.Client)
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()

    for {
        select {
        case action := <-actions:
            switch action.Action {
            case "add":
                clients[action.ID] = action.Client
            case "remove":
                delete(clients, action.ID)
            }
        case <-ticker.C:
            // Your original matchmaking logic
            Matchmaking(&clients)
        }
    }
}

func Matchmaking(clients *map[string]*cl.Client) {
    // Collect only players waiting for a match
    waiting := make([]*cl.Client, 0)
    waitingIDs := make([]string, 0)
    
    for id, client := range *clients {
        if client.State == cl.StateWaiting {
            waiting = append(waiting, client)
            waitingIDs = append(waitingIDs, id)
        }
    }

    // Match players in pairs (first-come-first-served)
    for i := 0; i+1 < len(waiting); i += 2 {
        player_1 := waiting[i]
        player_2 := waiting[i+1]

        // Set up the match (replace old opponents, not append)
        player_1.Opponents = []*cl.Client{player_2}
        player_2.Opponents = []*cl.Client{player_1}

        player_1.State = cl.StatePlaying
        player_2.State = cl.StatePlaying

        go StartGame(player_1, player_2, "game_text.txt")

        // Remove from matchmaking queue
        delete(*clients, waitingIDs[i])
        delete(*clients, waitingIDs[i+1])
    }
}

type GameSession struct {
    player1          *cl.Client
    player2          *cl.Client
    gameText         string
    player1Progress  int
    player2Progress  int
    player1WPM       float64
    player2WPM       float64
    mu               sync.Mutex
    gameOver         bool
}

func StartGame(player1, player2 *cl.Client, textFilePath string) error {
    // Read the text from file
    content, err := os.ReadFile(textFilePath)
    if err != nil {
        return fmt.Errorf("failed to read text file: %v", err)
    }
    
    gameText := strings.TrimSpace(string(content))
    
    // Create game session
    session := &GameSession{
        player1:  player1,
        player2:  player2,
        gameText: gameText,
    }
    
    // Send game start message with future start time
    startTime := time.Now().Add(3 * time.Second).Unix()
    message := map[string]interface{}{
        "type":        "game_start",
        "text":        gameText,
        "start_at":    startTime,
        "text_length": len(gameText),
    }
    
    messageJSON, err := json.Marshal(message)
    if err != nil {
        return fmt.Errorf("failed to marshal message: %v", err)
    }
    
    // Send to both players
    if err := player1.Conn.WriteMessage(websocket.TextMessage, messageJSON); err != nil {
        return fmt.Errorf("failed to send to player1: %v", err)
    }
    if err := player2.Conn.WriteMessage(websocket.TextMessage, messageJSON); err != nil {
        return fmt.Errorf("failed to send to player2: %v", err)
    }
    
    // Handle game session in goroutines for each player
    var wg sync.WaitGroup
    wg.Add(2)
    
    // Player 1's game loop
    go func() {
        defer wg.Done()
        session.handlePlayerGame(player1, true)
    }()
    
    // Player 2's game loop
    go func() {
        defer wg.Done()
        session.handlePlayerGame(player2, false)
    }()
    
    wg.Wait()
    
    // Game finished - reset states
    player1.State = cl.StateWaiting
    player2.State = cl.StateWaiting
    
    return nil
}

func (s *GameSession) handlePlayerGame(player *cl.Client, isPlayer1 bool) {
    for {
        s.mu.Lock()
        if s.gameOver {
            s.mu.Unlock()
            break
        }
        s.mu.Unlock()
        
        _, message, err := player.Conn.ReadMessage()
        if err != nil {
            // Player disconnected
            s.handleDisconnect(isPlayer1)
            break
        }
        
        // Parse the message
        var msg map[string]interface{}
        if err := json.Unmarshal(message, &msg); err != nil {
            continue
        }
        
        msgType, ok := msg["type"].(string)
        if !ok {
            continue
        }
        
        switch msgType {
        case "progress":
            // Get progress and WPM
            charsTyped, ok1 := msg["characters_typed"].(float64)
            wpm, ok2 := msg["wpm"].(float64)
            
            if !ok1 || !ok2 {
                continue
            }
            
            s.mu.Lock()
            
            // Update this player's progress
            if isPlayer1 {
                s.player1Progress = int(charsTyped)
                s.player1WPM = wpm
            } else {
                s.player2Progress = int(charsTyped)
                s.player2WPM = wpm
            }
            
            // Broadcast WPM to opponent
            opponent := s.player2
            if !isPlayer1 {
                opponent = s.player1
            }
            s.broadcastWPM(opponent, wpm)
            
            // Check if this player completed the text
            textLength := len(s.gameText)
            if int(charsTyped) >= textLength && !s.gameOver {
                s.gameOver = true
                s.mu.Unlock()
                s.endGame(isPlayer1)
                return
            }
            
            s.mu.Unlock()
        }
    }
}

func (s *GameSession) broadcastWPM(client *cl.Client, wpm float64) {
    message := map[string]interface{}{
        "type": "opponent_wpm",
        "wpm":  wpm,
    }
    messageJSON, _ := json.Marshal(message)
    client.Conn.WriteMessage(websocket.TextMessage, messageJSON)
}

func (s *GameSession) handleDisconnect(isPlayer1 bool) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    if s.gameOver {
        return
    }
    
    s.gameOver = true
    
    opponent := s.player2
    if !isPlayer1 {
        opponent = s.player1
    }
    
    message := map[string]interface{}{
        "type": "opponent_disconnected",
    }
    messageJSON, _ := json.Marshal(message)
    opponent.Conn.WriteMessage(websocket.TextMessage, messageJSON)
}

func (s *GameSession) endGame(player1Won bool) {
    var winner, loser *cl.Client
    var winnerWPM, loserWPM float64
    
    if player1Won {
        winner = s.player1
        loser = s.player2
        winnerWPM = s.player1WPM
        loserWPM = s.player2WPM
    } else {
        winner = s.player2
        loser = s.player1
        winnerWPM = s.player2WPM
        loserWPM = s.player1WPM
    }
    
    // Send win message
    winMessage := map[string]interface{}{
        "type":         "game_over",
        "result":       "win",
        "your_wpm":     winnerWPM,
        "opponent_wpm": loserWPM,
    }
    winJSON, _ := json.Marshal(winMessage)
    winner.Conn.WriteMessage(websocket.TextMessage, winJSON)
    
    // Send lose message
    loseMessage := map[string]interface{}{
        "type":         "game_over",
        "result":       "lose",
        "your_wpm":     loserWPM,
        "opponent_wpm": winnerWPM,
    }
    loseJSON, _ := json.Marshal(loseMessage)
    loser.Conn.WriteMessage(websocket.TextMessage, loseJSON)
}




