package main

import (
	"flag"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"strconv"
	"sync"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

var upgrader = websocket.Upgrader{} // use default options

type Position struct {
	X bool
	O bool
}

func (s Position) isX() bool {
	return s.X == true
}

func (s Position) isO() bool {
	return s.O == true
}

func (s Position) isEmpty() bool {
	return s.X == false && s.O == false
}

func (game Game) getRepresentation() string {
	repr := ""
	for index, pos := range game.Board {
		if pos.isEmpty() {
			repr += "_"
		} else if pos.isX() {
			repr += "X"
		} else if pos.isO() {
			repr += "Y"
		}
		if (index+1)%3 == 0 {
			repr += "\n"
		} else {
			repr += "|"
		}
	}
	return repr
}

func (game Game) printBoard() {
	log.Println(game.getRepresentation())
}

type Game struct {
	// represent state of the board
	Board [9]Position

	// game ends when Moves == 9
	Moves int8

	// maintain connections for both players
	PlayerOne *Player
	PlayerTwo *Player

	// locking mechanism for operations on the board
	mutex sync.RWMutex
}

func (g *Game) getCurrentPlayer() int8 {
	return g.Moves % int8(2)
}

type Player struct {
	// todo: might need refernce to game to remove itself on connection close/death?
	connection *websocket.Conn
	moves      chan int8
}

func (game *Game) run() {
	for {
		select {
		case playerOneMove := <-game.PlayerOne.moves:
			// todo: check if board position is not already occuppied
			if game.getCurrentPlayer() != 0 {
				sendMessage(game.PlayerOne.connection, []byte("Not your turn!"))
			} else {
				game.Board[playerOneMove] = Position{true, false}
				game.Moves += 1
				game.printBoard()
			}
			// log.Println("hello1:", playerOneMove)
		case playerTwoMove := <-game.PlayerTwo.moves:
			if game.getCurrentPlayer() != 1 {
				sendMessage(game.PlayerTwo.connection, []byte("Not your turn!"))
			} else {
				game.Board[playerTwoMove] = Position{false, true}
				game.Moves += 1
				game.printBoard()
			}
			// log.Println("hello2:", playerTwoMove)
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

func echo(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer c.Close()

	var p *Player
	if master.Games["game1"].PlayerOne == nil {
		p = &Player{c, make(chan int8)}
		master.Games["game1"].PlayerOne = p
		sendMessage(c, []byte("You are player one!"))
	} else if master.Games["game1"].PlayerTwo == nil {
		p = &Player{c, make(chan int8)}
		master.Games["game1"].PlayerTwo = p
		sendMessage(c, []byte("You are player two!"))
		go (master.Games["game1"]).run()
	} else {
		p = nil
		sendMessage(c, []byte("Cannot join this game!"))
	}

	if p == nil {
		return
	}

	for {
		_, message, err := c.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			break
		}
		val, err := strconv.Atoi(string(message))
		if err != nil {
			log.Println("invalid input:", err)
			// break
		} else {
			p.moves <- int8(val)
		}
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	master.Games = make(map[string]*Game)
	master.Games["game1"] = &Game{}

	log.Println("Starting...")
	flag.Parse()
	log.SetFlags(0)
	http.HandleFunc("/echo", echo)
	http.HandleFunc("/", home)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
