package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	//"strconv"
	"sync"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

var upgrader = websocket.Upgrader{} // use default options

type State int8

const (
	X State = 1
	O State = 2
)

func (s State) isX() bool {
	return s == X
}

func (s State) isO() bool {
	return s == O
}

func (s State) isEmpty() bool {
	return !s.isX() && !s.isO()
}

// for debugging purposes
func (game *Game) getRepresentation() string {
	repr := ""
	for index, pos := range game.Board {
		if pos.isEmpty() {
			repr += "_"
		} else if pos.isX() {
			repr += "X"
		} else if pos.isO() {
			repr += "O"
		}
		if (index+1)%3 == 0 {
			repr += "\n"
		} else {
			repr += "|"
		}
	}
	return repr
}

func (game *Game) printBoard() {
	log.Println(game.getRepresentation())
}

type Game struct {
	// represent state of the board
	Board [9]State

	// game ends when Moves == 9
	Moves int8

	// maintain connections for both players
	PlayerOne *Player
	PlayerTwo *Player

	// to send to players
	Broadcast chan SystemMessage

	// messages or moves incoming from user
	Inputs chan InternalUserMessage

	// locking mechanism for operations on the board
	mutex sync.RWMutex
}

func createGame() *Game {
	g := Game{}
	g.Broadcast = make(chan SystemMessage)
	g.Inputs = make(chan InternalUserMessage)
	return &g
}

func (game *Game) getCurrentPlayer() State {
	if game.Moves%int8(2) == 0 {
		return X
	} else {
		return O
	}
}

// UserMessage incoming payload from user
type UserMessage struct {
	//Move     State `json:"move"`
	Position int `json:"position"`

	// for trash-talk
	Message string `json:"message"`
}

type InternalUserMessage struct {
	message UserMessage
	player  *Player
}

// SystemMessage response from server
// Represents game state, current player and any message
type SystemMessage struct {
	Board         [9]State `json:"board"`
	CurrentPlayer State    `json:"current_player"`
	Message       string   `json:"message"`
}

func makeSystemMessage(game *Game, message string) SystemMessage {
	return SystemMessage{game.Board, game.getCurrentPlayer(), message}
}

type Player struct {
	connection *websocket.Conn

	// whether player is X or O
	char  State
	moves chan UserMessage
}

// Determine if board is in an end-state
func (game *Game) isDone() bool {
	return false
}

// Assumes player-check is done by caller
func (game *Game) update(pos int, move State) (bool, error) {
	if pos > len(game.Board) {
		return false, errors.New("not found")
	} else if !game.Board[pos].isEmpty()  {
		return false, errors.New("this seat is taken")
	} else {
		game.Board[pos] = move
		game.Moves++
		return true, nil
	}
}

func (game *Game) connections() []*websocket.Conn {
	var connections []*websocket.Conn
	if game.PlayerOne != nil {
		connections = append(connections, game.PlayerOne.connection)
	}
	if game.PlayerTwo != nil {
		connections = append(connections, game.PlayerTwo.connection)
	}
	return connections
}

func (game *Game) processInputs() {
	for {
		select {
		case i := <-game.Inputs:
			if i.player.char == game.getCurrentPlayer() &&
				!game.isDone() {
				if _, err := game.update(i.message.Position, i.player.char); err != nil {
					sendMessage(i.player.connection, []byte(fmt.Sprint(err)))
				} else {
					game.Broadcast <- makeSystemMessage(game, "sup!")
				}
			} else {
				if game.isDone() {
					sendMessage(i.player.connection, []byte("Game's done, go home!"))
				} else {
					sendMessage(i.player.connection, []byte("not your turn yet!"))
				}
			}
		}
	}
}

func (game *Game) processBroadcast() {
	for {
		select {
		case b := <-game.Broadcast:
			for _, c := range game.connections() {
				payload, _ := json.Marshal(b)
				sendMessage(c, payload)
			}
		}
	}
}

type Master struct {
	// keep track of all games on the server
	Games map[string]*Game
}

func sendMessage(con *websocket.Conn, message []byte) {
	err := con.WriteMessage(websocket.TextMessage, message)
	if err != nil {
		log.Println("Error writing message:", err)
	}
}

var master = Master{}

func createPlayer(conn *websocket.Conn, char State) *Player {
	return &Player{conn, char, make(chan UserMessage)}
}

func play(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	defer c.Close()

	var p *Player
	game := master.Games["game1"]
	if game.PlayerOne == nil {
		p = createPlayer(c, X)
		game.PlayerOne = p
		game.Broadcast <- makeSystemMessage(game, "Player one has joined")
	} else if game.PlayerTwo == nil {
		p = createPlayer(c, O)
		game.PlayerTwo = p
		game.Broadcast <- makeSystemMessage(game, "Player two has joined, let's begin!")
	} else {
		p = nil
		sendMessage(c, []byte("Cannot join this game!"))
		return
	}

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		var msg UserMessage
		if err = json.Unmarshal(message, &msg); err != nil {
			sendMessage(c, []byte("Invalid message format!"))
		} else {
			game.Inputs <- InternalUserMessage{msg, p}
		}
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	master.Games = make(map[string]*Game)
	master.Games["game1"] = createGame()
	go (master.Games["game1"]).processInputs()
	go (master.Games["game1"]).processBroadcast()

	log.Println("Starting...")
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/play", play)
	http.HandleFunc("/", home)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
