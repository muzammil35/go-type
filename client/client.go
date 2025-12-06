package client

import (
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type ClientState string

const (
    StateWaiting ClientState = "waiting"
    StatePlaying ClientState = "playing"
    StateDone    ClientState = "done"
)


type Client struct {
	name string
	Id string
	Conn *websocket.Conn
	wpm uint16
	Opponents []*Client
	State ClientState
	

}

func GenerateUserID() string {
    return uuid.New().String()
}



func NewClient(name string, id string, conn *websocket.Conn) *Client {
	return &Client{
		name: name,
		Id:   GenerateUserID(),
		conn: conn,
		wpm:0,
		State: StateWaiting,
	}
}

