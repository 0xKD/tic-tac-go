package main

// UserMessage incoming payload from user
type UserMessage struct {
	Command  Command `json:"command"`
	Position int     `json:"position"`
	GameId   string  `json:"game_id"`

	// for trash-talk
	Message string `json:"message"`
}

type Command string

const (
	CREATE  Command = "CREATE"
	JOIN            = "JOIN"
	PLAY            = "PLAY"
	TALK            = "TALK"
	REMATCH         = "REMATCH"
)

type InternalUserMessage struct {
	message UserMessage
	player  *Player
}

type MessageType string

const (
	INFO    MessageType = "INFO"
	WARNING             = "WARNING"
	ERROR               = "ERROR"
	CHAT                = "CHAT"
)

// SystemResponse response from server
// Represents game state, current player and any message
type SystemResponse struct {
	GameId        string             `json:"game_id"`
	RematchId     string             `json:"rematch_id"`
	Board         [SIZE * SIZE]State `json:"board"`
	CurrentPlayer State              `json:"current_player"`
	Char          State              `json:"char"`
	Message       string             `json:"message"`
	MessageType   MessageType        `json:"message_type"`
	GameOver      bool               `json:"game_over"`
	Winner        State              `json:"winner"`
}
