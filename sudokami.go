/*
Command sudokami solves sudoku puzzles through concurrent deduction.

It launches a goroutine for each candidate inference in the puzzle to listen on a channel for deductions made by
the inferences in the same cell or of the same digit in the same row, column, or 3x3 box (with which it is mutually exclusive).
When a candidate deduces its necessity or falsehood, it informs the groups it belongs to.
When a group determines that one of its candidates is true, it informs its candidates.
In this way, command sudokami can solve any valid puzzle that depends only on single candidate inference patterns.

Usage:
Pass the puzzle as a string argument on the command line:
	go run sudokami.go "..2.3...8.....8....31.2.....6..5.27..1.....5.2.4.6..31....8.6.5.......13..531.4.."

The digits 1-9 represent clues, and 0 or . denote empty cells. All other characters are ignored.
*/
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
)

const (
	// m is the number of cells spanning each box, and the number of boxes spanning the puzzle grid.
	// For a traditional sudoku puzzle, m == 3.
	m = 3

	// d is the number of cells spanning the puzzle grid, and the number of digits used in the puzzle.
	// It is also the number of Candidates in each Group.
	d = m * m

	// nGroups is the number of Groups a Candidate belongs to.
	// Each Candidate represents a unique combination of one row, one column, and one digit.
	// In addition, each Candidate belongs to one box.
	nGroups = 4
)

func main() {
	in := os.Args[1]
	s, err := parseInput(in)
	if err != nil {
		panic(err)
	}

	var wg sync.WaitGroup
	g := NewGrid(&wg)
	for i, n := range s {
		if n != 0 {
			g.Clue(i/d, i%d, n-1)
		}
	}
	wg.Wait()
	fmt.Println(g)
}

// Candidate is an inference regarding a single candidate digit in a single cell.
type Candidate struct {
	// ch is used to receive information about the Candidate deduced by its Groups.
	// The first value the Candidate receives on ch informs it whether its inference is in the puzzle's solution.
	ch chan bool

	// groups holds the channels of the Candidate's Groups.
	// When the Candidate determines the truth value of its inference, it sends this value to each of its Groups.
	groups []chan bool

	// value records the first value received on ch.
	value bool
}

// NewCandidate creates a new Candidate, launches a goroutine for it to receive and send values, and returns a pointer to it.
func NewCandidate(wg *sync.WaitGroup) *Candidate {
	c := &Candidate{
		// ch will receive one value from each Group and possibly one value from NewGrid.
		// Buffer it with sufficient capacity so that sends on it never block.
		ch:     make(chan bool, nGroups+1),
		groups: make([]chan bool, 0, nGroups),
	}

	// Listen on ch. The first value received indicates the truth value of c's inference.
	// Report this value to c's Groups, then call wg.Done and return: since ch is buffered
	// to sufficient capacity, it is not necessary to consume the remaining values sent.
	wg.Add(1)
	go func() {
		c.value = <-c.ch
		sendAll(c.groups, c.value)
		wg.Done()
	}()

	return c
}

// Group supervises all of the Candidates in a single cell,
// or all of the Candidates of a single digit in a single row, column, or box,
// precisely one of which is true in a puzzle's solution.
type Group struct {
	// ch is used to receive the deductions of the Group's Candidates.
	// When each Candidate determines the truth value of its inference, it sends this value on ch.
	ch chan bool

	// cans holds the channels of the Group's Cardidates.
	// When the Group determines the truth value of the Candidate(s) yet to report to it,
	// it sends this value to all of its Candidates.
	cans []chan bool

	// n is the number of Candidate inferences that have not been determined to be false.
	n int
}

// NewGroup creates a new Group, launches a goroutine for it to receive and send values, and returns a pointer to it.
func NewGroup() *Group {
	// nCan is the number of Candidates in a Group.
	const nCan = d

	g := &Group{
		// ch will receive one value from each Candidate.
		// Buffer it with sufficient capacity so that sends on it never block.
		ch:   make(chan bool, nCan),
		cans: make([]chan bool, 0, nCan),
		n:    nCan,
	}

	// Listen on ch. When a Candidate indicates that it is false, decrement n.
	// Continue until one of the following conditions is fulfilled:
	// 1. If a Candidate indicates that it is true, all others must be false: send false to all Candidates and return.
	// 2. If n reaches 1, the remaining Candidate must be true: send true to all Candidates and return.
	go func() {
		for g.n > 1 {
			if <-g.ch {
				sendAll(g.cans, false)
				return
			}
			g.n--
		}
		sendAll(g.cans, true)
	}()

	return g
}

// Grid is a collection of Candidates comprising a puzzle, and the Groups they belong to.
type Grid struct {
	cs []*Candidate
	gs []*Group
}

// NewGrid creates a new Grid, creates and connects its Candidates and Groups, and returns a pointer to it.
func NewGrid(wg *sync.WaitGroup) *Grid {
	g := &Grid{
		cs: make([]*Candidate, d*d*d),   // row * d^2 + column * d + digit
		gs: make([]*Group, nGroups*d*d), // row-column, row-digit, column-digit, box-digit
	}
	for i := range g.cs {
		g.cs[i] = NewCandidate(wg)
	}
	for i := range g.gs {
		g.gs[i] = NewGroup()
	}
	for i, ci := range g.cs {
		connect := func(gr *Group) {
			ci.groups = append(ci.groups, gr.ch)
			gr.cans = append(gr.cans, ci.ch)
		}
		connect(g.gs[0*d*d+row(i)*d+col(i)])
		connect(g.gs[1*d*d+row(i)*d+dig(i)])
		connect(g.gs[2*d*d+col(i)*d+dig(i)])
		connect(g.gs[3*d*d+box(i)*d+dig(i)])
	}

	return g
}

// Clue records that the cell in the given row and column contains the given digit,
// with all parameters in the range [0, d-1].
func (g *Grid) Clue(row, column, digit int) { g.cs[index(row, column, digit)].ch <- true }

// String returns a string representation of g.
func (g *Grid) String() string {
	var s string
	for r := 0; r < d; r++ {
		for c := 0; c < d; c++ {
			var n, count int
			for i := 0; i < d; i++ {
				if g.cs[index(r, c, i)].value {
					count++
					n = i
				}
			}
			if count == 1 {
				s += strconv.Itoa(n + 1)
			} else {
				s += "."
			}
		}
		s += "\n"
	}
	return s
}

// sendAll sends b on all channels in chans.
func sendAll(chans []chan bool, b bool) {
	for _, ch := range chans {
		ch <- b
	}
}

// index returns a Candidate's index.
func index(row, column, digit int) int { return row*d*d + column*d + digit }

func row(n int) int { return n / (d * d) }
func col(n int) int { return n / d % d }
func dig(n int) int { return n % d }
func box(n int) int {
	//  0 | 1 | 2
	// ---+---+---
	//  3 | 4 | 5
	// ---+---+---
	//  6 | 7 | 8
	return row(n)/m*m + col(n)/m
}

// parseInput parses an input string.
// It accepts the digits 1-9 and 0 or . to denote empty cells.
// It returns an error if input does not contain exactly d^2 of these.
// All other characters are ignored.
func parseInput(input string) ([]int, error) {
	s := make([]int, 0, d*d)
	for _, b := range input {
		switch {
		case b == '0', b == '.':
			s = append(s, 0)
		case '1' <= b && b <= '9':
			s = append(s, int(b-'0'))
		}
	}
	if len(s) != d*d {
		return nil, errors.New("invalid puzzle length")
	}
	return s, nil
}
