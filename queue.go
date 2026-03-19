package main

// queue is a FIFO of lines waiting to be released by --delay.
type queue struct {
	items [][]byte
	head  int
}

func (q *queue) len() int { return len(q.items) - q.head }

func (q *queue) push(line []byte) { q.items = append(q.items, line) }

// pop removes and returns the oldest line. The queue must not be empty.
func (q *queue) pop() []byte {
	line := q.items[q.head]
	q.items[q.head] = nil
	q.head++
	switch {
	case q.head == len(q.items):
		q.items, q.head = q.items[:0], 0
	case q.head >= 1024 && q.head*2 >= len(q.items):
		// Slide the live items down so a queue that never quite empties
		// doesn't keep growing its backing array.
		n := copy(q.items, q.items[q.head:])
		clear(q.items[n:])
		q.items, q.head = q.items[:n], 0
	}
	return line
}
