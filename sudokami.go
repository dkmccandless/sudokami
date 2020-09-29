/*
Command sudokami solves sudoku puzzles through concurrent deduction.

It launches a goroutine for each cell in the puzzle to listen on a channel for deductions made by the cells adjacent to it
in the same row, column, or 3x3 box. When a cell deduces the digit it must contain, it sends this digit on the channels
of the adjacent cells. In this way, command sudokami can solve any puzzle that depends only on sole candidate inference.

Usage:
Pass the puzzle as a string argument on the command line:
	go run sudokami.go "3.542.81.4879.15.6.29.5637485.793.416132.8957.74.6528.2413.9.655.867.192.965124.8"

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
	g := NewGrid(s, &wg)
	wg.Wait()
	fmt.Println(g)
}

// Cell is a cell in a sudoku puzzle.
type Cell struct {
	// can holds the candidate digits that the Cell might contain.
	can map[int]struct{}

	// ch is used to receive digits that the Cell cannot contain.
	ch chan int

	// adj is a list of channels on which to send the digit the Cell contains, when that digit is deduced.
	adj []chan int
}

// String returns a string representation of c.
func (c *Cell) String() string {
	if len(c.can) == 1 {
		for n := range c.can {
			return strconv.Itoa(n)
		}
	}
	return "."
}

// NewCell creates a new Cell, launches a goroutine for it to receive and send values, and returns a pointer to it.
func NewCell(wg *sync.WaitGroup) *Cell {
	// nAdj is the number of Cells adjacent to any Cell.
	// A Cell shares its box with m^2-1 others. Besides these, there are m(m-1) other Cells in its row, and m(m-1) others in its column.
	const nAdj = m*m - 1 + 2*m*(m-1)

	c := &Cell{
		can: make(map[int]struct{}),
		ch:  make(chan int, nAdj), // Buffer ch with enough capacity to receive one value from each adjacent Cell
		adj: make([]chan int, 0, nAdj),
	}
	for n := 1; n <= d; n++ {
		c.can[n] = struct{}{}
	}

	// Receive values over ch and update can. When there is only one remaining candidate,
	// announce that value to all adjacent Cells, then call wg.Done and return.
	// Since the number of values each goroutine might receive from those adjacent to it is known,
	// and each channel is buffered to sufficient capacity, the goroutine can return after sending
	// without consuming the rest of the values sent to it.
	wg.Add(1)
	go func() {
		for len(c.can) > 1 {
			delete(c.can, <-c.ch)
		}
		for n := range c.can {
			for _, ch := range c.adj {
				ch <- n
			}
		}
		wg.Done()
	}()

	return c
}

// Grid is a collection of Cells comprising a puzzle.
type Grid struct{ cs []*Cell }

// NewGrid creates a new Grid, populates its Cells according to s, and returns a pointer to it.
func NewGrid(digits []int, wg *sync.WaitGroup) *Grid {
	g := &Grid{cs: make([]*Cell, d*d)}
	for i := range g.cs {
		g.cs[i] = NewCell(wg)
	}
	for i, ci := range g.cs {
		for j, cj := range g.cs {
			if !isAdjacent(i, j) {
				continue
			}
			// Add cj's channel to ci's list
			ci.adj = append(ci.adj, cj.ch)
		}
	}

	for i, n := range digits {
		if n == 0 {
			continue
		}
		// Assign n to g.cs[i]
		for not := 1; not <= d; not++ {
			if not == n {
				continue
			}
			g.cs[i].ch <- not
		}
	}
	return g
}

// String returns a string representation of g.
func (g *Grid) String() string {
	var s string
	for i := 0; i < d; i++ {
		for j := 0; j < d; j++ {
			c := g.cs[d*i+j]
			s += c.String()
		}
		s += "\n"
	}
	return s
}

// isAdjacent reports whether i and j index adjacent Cells.
// Two cells are adjacent if they are in the same row, column, or box but are not identical.
func isAdjacent(i, j int) bool {
	return i != j && (row(i) == row(j) || col(i) == col(j) || box(i) == box(j))
}

// row returns the row number of n.
func row(n int) int { return n / d }

// col returns the column number of n.
func col(n int) int { return n % d }

// box returns the box number of n.
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
