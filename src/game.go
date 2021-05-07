package main

import (
	"errors"
	"log"
	"sync"
)

type State int8

const (
	EMPTY State = 0
	X           = 1
	O           = 2
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

type Sol struct {
	s State

	// set to -1 to invalidate
	count int8
}

const SIZE = 3

type Game struct {
	// represent state of the board
	Board    [SIZE * SIZE]State
	Solution [SIZE*SIZE + 2]Sol

	Moves int8

	// game ends when Moves == (SIZE * SIZE) or when someone wins
	Done bool

	// locking mechanism for operations on the board
	mutex sync.RWMutex
}

func (game *Game) getCurrentPlayer() State {
	if game.Moves%int8(2) == 0 {
		return X
	} else {
		return O
	}
}

func (game *Game) isOver() bool {
	return game.Moves == int8(len(game.Board))
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

// returns (bool, error)
// bool = true if "move" at "pos" resulted in a win, false otherwise
// error !=nil if the move is illegal (outside the board, or if position on board already exists)
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
