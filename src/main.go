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

const SIZE = 3

type Sol struct {
	s State

	// set to -1 to invalidate
	count int8
}

type Game struct {
	// represent state of the board
	Board    [SIZE * SIZE]State
	Solution [SIZE*SIZE + 2]Sol

	Moves int8

	// game ends when Moves == (SIZE * SIZE) or when someone wins
	Done bool

	// maintain connections for both players
	PlayerOne *Player
	PlayerTwo *Player

	// to send to players
	Broadcast chan BroadcastMessage

	// messages or moves incoming from user
	Inputs chan InternalUserMessage

	// locking mechanism for operations on the board
	mutex sync.RWMutex
}

func createGame() *Game {
	g := Game{}
	g.Broadcast = make(chan BroadcastMessage)
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

// BroadcastMessage response from server
// Represents game state, current player and any message
type BroadcastMessage struct {
	Board         [SIZE * SIZE]State `json:"board"`
	CurrentPlayer State              `json:"current_player"`
	Message       string             `json:"message"`
}

func (game *Game) broadcast(message string) {
	msg := BroadcastMessage{game.Board, game.getCurrentPlayer(), message}
	game.Broadcast <- msg
}

type Player struct {
	connection *websocket.Conn

	// whether player is X or O
	char  State
	moves chan UserMessage
}

func getCharText(move State) string {
	if move == X {
		return "X"
	} else if move == O {
		return "O"
	} else {
		return "?"
	}
}

// returns true if board is in end-state and "move" has won
func (game *Game) updateSolution(pos int, move State) bool {
	x, y := pos/SIZE, pos%SIZE
	row, col := x, y+SIZE
	var toCheck = []int{row, col}

	// add check for SIZE % 2 == 1 if it becomes dynamic
	flippedY := 2*(SIZE/2) - y
	if x == y {
		toCheck = append(toCheck, len(game.Solution)-1)
	}
	if x == flippedY {
		toCheck = append(toCheck, len(game.Solution)-2)
	}

	for _, v := range toCheck {
		//log.Printf(
		//	"fin. v=%d, pos=%d, move=%s (x=%d, y=%d, fY=%d)",
		//	v, pos, getCharText(move), x, y, flippedY)

		if game.Solution[v].count == -1 {
			continue
		}

		if game.Solution[v].count == 0 {
			// initial condition
			game.Solution[v] = Sol{move, 1}
		} else if game.Solution[v].s != move {
			// contains some other state already, invalidate
			game.Solution[v].count = -1
		} else {
			game.Solution[v].count += 1
			if game.Solution[v].count >= SIZE {
				return true
			}
		}
	}
	return false
}

// Assumes player-check is done by caller
func (game *Game) update(pos int, move State) (bool, error) {
	if pos > len(game.Board) {
		return false, errors.New("not found")
	} else if !game.Board[pos].isEmpty() {
		return false, errors.New("this seat is taken")
	} else {
		game.Board[pos] = move
		game.Moves++
		return game.updateSolution(pos, move), nil
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
			if len(game.connections()) != 2 {
				game.broadcast("Wait for all players to join!")
			} else if i.player.char == game.getCurrentPlayer() && !game.Done {
				if finished, err := game.update(i.message.Position, i.player.char); err != nil {
					sendMessage(i.player.connection, []byte(fmt.Sprint(err)))
				} else {
					game.Done = finished || game.Moves == int8(len(game.Board))
					var text string
					if finished {
						text = getCharText(i.player.char) + " has won!"
					} else {
						text = "..."
					}
					game.broadcast(text)
				}
			} else {
				if game.Done {
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

	var p *Player
	game := master.Games["game1"]

	defer func(c *websocket.Conn, game *Game) {
		err := c.Close()
		if err != nil {
			log.Println("Error closing websocket:", err)
		}

		if game.PlayerOne != nil && game.PlayerOne.connection == c {
			//log.Println("closing..")
			game.PlayerOne = nil
			game.broadcast("Someone left the game...")
		} else if game.PlayerTwo != nil && game.PlayerTwo.connection == c {
			//log.Println("closing..")
			game.PlayerTwo = nil
			game.broadcast("Someone left the game...")
		}
	}(c, game)

	if game.PlayerOne == nil {
		p = createPlayer(c, X)
		game.PlayerOne = p
		game.broadcast("Player one has joined")
	} else if game.PlayerTwo == nil {
		p = createPlayer(c, O)
		game.PlayerTwo = p
		game.broadcast("Player two has joined, let's begin!")
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
