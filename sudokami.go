/*
Command sudokami solves sudoku puzzles through concurrent deduction.

It launches a goroutine for each candidate inference in the puzzle to listen on a channel for deductions made by
the inferences in the same cell or of the same digit in the same row, column, or 3x3 box (with which it is mutually exclusive).
When a candidate deduces its necessity or falsehood, it informs the candidates adjacent to it or the groups it belongs to, respectively.
When a group determines that exactly one of its candidates can be true, it informs its candidates.
In this way, command sudokami can solve any puzzle that depends only on single candidate inference patterns.

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
	d = m * m
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
			g.Clue(i/9, i%9, n-1)
		}
	}
	wg.Wait()
	fmt.Println(g)
}

// Candidate is an inference regarding a single candidate digit in a single cell.
type Candidate struct {
	// ch is used to receive information about the Candidate deduced by its Groups and adjacent Candidates.
	// The first value the Candidate receives on ch informs it whether its inference is in the puzzle's solution.
	ch chan bool

	// ach holds the channels of adjacent Candidates.
	// If the Candidate's inference is true, it sends false to all adjacent Candidates.
	ach []chan bool

	// gch holds the channels of the Groups the Candidate belongs to.
	// If the Candidate's inference is false, it sends false to all of its Groups.
	gch []chan bool

	// b records whether the Candidate has determined that its inference is true.
	b bool
}

// NewCandidate creates a new Candidate, launches a goroutine for it to receive and send values, and returns a pointer to it.
func NewCandidate(wg *sync.WaitGroup) *Candidate {
	// nAdj is the number of adjacent Candidates.
	// A Candidate shares its cell with m^2-1 other digits,
	// its row with m^2-1 occurrences of the same digit in different columns,
	// and its column with m^2-1 occurrences of the same digit in different rows.
	// Besides these, there are (m-1)^2 other occurrences of the same digit in a different row and column of the same box.
	const nAdj = 3*(m*m-1) + (m-1)*(m-1)

	// nGroup is the number of Groups a Candidate belongs to.
	// Each Candidate belongs to a unique combination of one row, one column, and one digit.
	// In addition, each Candidate belongs to one box.
	const nGroup = 4

	c := &Candidate{
		// Buffer ch with enough capacity to receive one value from NewGrid and one from
		// each Group and adjacent Candidate so that channel operations will never block.
		ch:  make(chan bool, nAdj+nGroup+1),
		ach: make([]chan bool, 0, nAdj),
		gch: make([]chan bool, 0, nGroup),
	}

	// Listen on ch. The first value received indicates the truth value of c's inference.
	// If true is received, send false to all adjacent Candidates; if false is received, inform the Groups.
	// In either case, call wg.Done and consume all remaining sends on ch without action.
	wg.Add(1)
	go func() {
		switch c.b = <-c.ch; c.b {
		case true:
			for _, a := range c.ach {
				a <- false
			}
		case false:
			for _, g := range c.gch {
				g <- false
			}
		}
		wg.Done()
		for {
			<-c.ch
		}
	}()

	return c
}

// Group supervises all of the Candidates in a single cell,
// or all of the Candidates of a single digit in a single row, column, or box,
// precisely one of which is true in a puzzle's solution.
type Group struct {
	// ch is used to receive the deductions of the Group's Candidates.
	// Each Candidate that determines that its inference is false sends false once on ch.
	ch chan bool

	// cch holds the channels of the Group's Cardidates.
	// When the Group has determined that only one of its Candidates can be true,
	// it sends true to all of its Candidates.
	cch []chan bool

	// n is the number of Candidate inferences that have not been determined to be false.
	n int
}

// NewGroup creates a new Group, launches a goroutine for it to receive and send values, and returns a pointer to it.
func NewGroup() *Group {
	// nCan is the number of Candidates in a Group.
	const nCan = d

	g := &Group{
		// Each of g's Candidates will send one value when it determines that it is false.
		ch:  make(chan bool, nCan-1),
		cch: make([]chan bool, 0, nCan),
		n:   nCan,
	}

	// Listen on ch. When a Candidate indicates that it is false, decrement n.
	// When n reaches 1, g has received complete information: send true to all Candidates and return.
	go func() {
		for g.n > 1 {
			<-g.ch
			g.n--
		}
		for _, c := range g.cch {
			c <- true
		}
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
		cs: make([]*Candidate, d*d*d), // row * d^2 + column * d + digit
		gs: make([]*Group, 4*d*d),     // row-column, row-digit, column-digit, box-digit
	}
	for i := range g.cs {
		g.cs[i] = NewCandidate(wg)
	}
	for i := range g.gs {
		g.gs[i] = NewGroup()
	}
	for i, ci := range g.cs {
		connect := func(gr *Group) {
			ci.gch = append(ci.gch, gr.ch)
			gr.cch = append(gr.cch, ci.ch)
		}
		connect(g.gs[0*d*d+row(i)*d+col(i)])
		connect(g.gs[1*d*d+row(i)*d+dig(i)])
		connect(g.gs[2*d*d+col(i)*d+dig(i)])
		connect(g.gs[3*d*d+box(i)*d+dig(i)])

		for j, cj := range g.cs {
			if !isAdjacent(i, j) {
				continue
			}
			// Add cj's channel to ci's list
			ci.ach = append(ci.ach, cj.ch)
		}
	}

	return g
}

// Clue records that the cell in the given row and column contains the given digit,
// with all parameters in the range [0, 8].
func (g *Grid) Clue(row, column, digit int) { g.cs[index(row, column, digit)].ch <- true }

// String returns a string representation of g.
func (g *Grid) String() string {
	var s string
	for r := 0; r < d; r++ {
		for c := 0; c < d; c++ {
			var n, count int
			for i := 0; i < d; i++ {
				if g.cs[index(r, c, i)].b {
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

// isAdjacent reports whether i and j index adjacent Candidates.
// Candidates are adjacent if they are distinct and have the same row and column, row and digit, column and digit, or box and digit.
func isAdjacent(i, j int) bool {
	if i == j {
		return false
	}
	if dig(i) != dig(j) {
		return row(i) == row(j) && col(i) == col(j)
	}
	return row(i) == row(j) || col(i) == col(j) || box(i) == box(j)
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
