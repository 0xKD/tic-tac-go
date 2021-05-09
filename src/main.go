package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
	"sync"
	"time"
	_ "time"
)

var addr = flag.String("addr", "localhost:8080", "http service address")

var upgrader = websocket.Upgrader{} // use default options

type Session struct {
	id        string
	rematchId string
	game      *Game

	// maintain players for both players
	PlayerX *Player
	PlayerO *Player

	// to send to players
	Broadcast chan SystemResponse

	// messages or moves incoming from user
	Inputs chan InternalUserMessage
}

func (session *Session) setPlayer(player *Player, c State) {
	if c == X {
		player.setChar(X)
		session.PlayerX = player
	} else if c == O {
		player.setChar(O)
		session.PlayerO = player
	} else {
		log.Printf("Got %d for setPlayer (!?)\n", c)
		return
	}
	player.session = session
}

func createGameId() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("cannot get random bytes")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (master *Master) newSession(player *Player) *Session {
	session := &Session{}
	session.game = &Game{}
	session.Broadcast = make(chan SystemResponse)
	session.Inputs = make(chan InternalUserMessage)
	session.setPlayer(player, X)
	session.id, _ = createGameId()
	master.sessions[session.id] = session

	// we need a waitGroup since we want the worker goroutine to finish
	wg := &sync.WaitGroup{}
	go session.processInputs(wg)
	go session.processBroadcast(wg)

	// kill after some time
	time.AfterFunc(120*time.Second, master.waitKill(session.id, wg))
	return session
}

func (master *Master) waitKill(gameId string, wg *sync.WaitGroup) func() {
	return func() {
		session, found := master.sessions[gameId]
		if !found {
			log.Println("Can't explain this..")
			return
		}

		session.warning("Game terminated game due to inactivity 💀 - Start a new one!")
		close(session.Broadcast)
		<-session.Broadcast

		close(session.Inputs)
		<-session.Inputs

		wg.Wait()
		for _, p := range session.players() {
			// this isn't ideal, but it also takes care of terminating the loop in "play()"
			closeWebsocket(p.conn, p, false)
		}

		// I assume this takes care of gc of everything relevant
		delete(master.sessions, gameId)
	}
}

func (session *Session) message(typ MessageType, text string) SystemResponse {
	return SystemResponse{
		session.id,
		session.rematchId,
		session.game.Board,
		session.game.getCurrentPlayer(),
		EMPTY,
		text,
		typ,
		session.game.isOver(),
		session.game.Winner,
	}
}

func systemResponse(typ MessageType, text string) SystemResponse {
	resp := SystemResponse{}
	resp.Message = text
	resp.MessageType = typ
	return resp
}

func (session *Session) broadcast(message string) {
	// send info message to all recipients
	session.Broadcast <- session.message(INFO, message)
}

func (session *Session) warning(message string) {
	session.Broadcast <- session.message(WARNING, message)
}

func sendMessage(con *websocket.Conn, message []byte) {
	err := con.WriteMessage(websocket.TextMessage, message)
	if err != nil {
		log.Println("Error writing message:", err)
	}
}

type Player struct {
	conn *websocket.Conn

	// whether player is X or O
	char    State
	moves   chan UserMessage
	session *Session
	rematch bool
}

func (player *Player) message(typ MessageType, text string) {
	var resp SystemResponse
	if player.session != nil {
		resp = player.session.message(typ, text)
	} else {
		resp = systemResponse(typ, text)
	}
	resp.Char = player.char
	payload, _ := json.Marshal(resp)
	sendMessage(player.conn, payload)
}

func (player *Player) info(text string) {
	player.message(INFO, text)
}

func (player *Player) warning(text string) {
	player.message(WARNING, text)
}

func (player *Player) error(text string) {
	player.message(ERROR, text)
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

func (session *Session) players() []*Player {
	var connections []*Player
	if session.PlayerX != nil {
		connections = append(connections, session.PlayerX)
	}
	if session.PlayerO != nil {
		connections = append(connections, session.PlayerO)
	}
	return connections
}

// goroutine to handle I/O on the game session
func (session *Session) processInputs(wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		select {
		case cmd, ok := <-session.Inputs:
			if !ok {
				return
			}

			if len(session.players()) != 2 {
				session.warning("Wait for all players to join!")
			} else if cmd.player.char == session.game.getCurrentPlayer() && !session.game.isOver() {
				if err := session.game.update(cmd.message.Position, cmd.player.char); err != nil {
					cmd.player.error(fmt.Sprint(err))
				} else {
					var text string
					if session.game.Winner != EMPTY {
						// this variation gets replaced on the front-end (checked using "has won")
						text = getCharText(cmd.player.char) + " has won!"
					} else if session.game.isOver() {
						text = "Game over! It's a draw 😔"
					} else {
						// game's still on, frontend will show an appropriate message
						text = ""
					}
					session.broadcast(text)
				}
			} else if cmd.message.Command == REMATCH {
				cmd.player.rematch = true
				if session.PlayerX.rematch && session.PlayerO.rematch && session.rematchId == "" {
					newSession := master.newSession(cmd.player)
					session.rematchId = newSession.id
					session.broadcast("") // frontend will show dynamic message/timer
				}
			} else {
				if session.game.isOver() {
					cmd.player.error("Game is over! Hit \"New Game\" to start another 🕹️")
				} else {
					cmd.player.error("It's not your turn yet 😠")
				}
			}
		}
	}
}

func (session *Session) processBroadcast(wg *sync.WaitGroup) {
	wg.Add(1)
	defer wg.Done()

	for {
		select {
		case resp, ok := <-session.Broadcast:
			//log.Println("broadcast:", resp)
			if !ok {
				return
			}

			for _, p := range session.players() {
				// set player char
				resp.Char = p.char
				payload, _ := json.Marshal(resp)
				sendMessage(p.conn, payload)
			}
		}
	}
}

func (session *Session) kick(c *websocket.Conn, communicate bool) {
	if session.PlayerX != nil && session.PlayerX.conn == c {
		session.PlayerX = nil
		if communicate {
			session.broadcast("X has left the game...")
		}
	} else if session.PlayerO != nil && session.PlayerO.conn == c {
		session.PlayerO = nil
		if communicate {
			session.broadcast("O has left the game...")
		}
	}
}

type Master struct {
	// keep track of all games on the server
	sessions map[string]*Session
}

var master = Master{}

func (player *Player) setChar(s State) {
	if !player.char.isEmpty() {
		log.Println("This shouldn't happen")
	}
	player.char = s
}

func (player *Player) joinGame(gameId string) {
	session, found := master.sessions[gameId]
	if !found {
		player.error("Game not found! Hit \"New Game\" to start one")
		return
	}

	if session.PlayerX == nil {
		session.setPlayer(player, X)
		player.info("You've joined the game! Share this page with someone to play against")
	} else if session.PlayerO == nil {
		// improve messages for various states of the game (not started, midway, over)
		session.setPlayer(player, O)
		player.info("You've joined the game! Let's go!")
		session.PlayerX.info("O has joined the game, let's go!")
	} else {
		player.error("Can't join this game!")
	}
}

func createPlayer(conn *websocket.Conn) *Player {
	return &Player{conn: conn, moves: make(chan UserMessage)}
}

func closeWebsocket(c *websocket.Conn, player *Player, communicate bool) {
	err := c.Close()
	if err != nil {
		log.Println("Error closing websocket:", err)
		// it may already be closed so don't bother sending a message
		communicate = false
	}

	if player.session != nil {
		player.session.kick(player.conn, communicate)
	}
}

func play(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}

	player := createPlayer(conn)
	defer closeWebsocket(conn, player, true)

	// below section can perhaps also be a goroutine to make cleanup easier
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error reading from ws:", err)
			return
		}

		var msg UserMessage
		if err = json.Unmarshal(message, &msg); err != nil {
			player.error("Invalid command")
			continue
		}

		switch msg.Command {
		case JOIN:
			player.joinGame(msg.GameId)
		case CREATE:
			session := master.newSession(player)
			player.info(fmt.Sprintf("Created game (id=%s)", session.id))
		case REMATCH:
			fallthrough
		case PLAY:
			if player.session != nil {
				player.session.Inputs <- InternalUserMessage{msg, player}
			}
		}
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func main() {
	master.sessions = make(map[string]*Session)

	log.Println("Starting...")
	flag.Parse()
	log.SetFlags(0)

	fs := http.FileServer(http.Dir("./static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))
	http.HandleFunc("/play", play)
	http.HandleFunc("/", home)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
