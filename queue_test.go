package main

import (
	"strconv"
	"testing"
)

func TestQueueIsFIFO(t *testing.T) {
	var q queue
	next, want := 0, 0
	// Interleave pushes and pops so the queue both empties and compacts.
	for round := range 50 {
		for range 100 + round*7 {
			q.push([]byte(strconv.Itoa(next)))
			next++
		}
		for range 90 + round*3 {
			if got := string(q.pop()); got != strconv.Itoa(want) {
				t.Fatalf("pop = %s, want %d", got, want)
			}
			want++
		}
		if q.len() != next-want {
			t.Fatalf("len = %d, want %d", q.len(), next-want)
		}
	}
	for q.len() > 0 {
		if got := string(q.pop()); got != strconv.Itoa(want) {
			t.Fatalf("pop = %s, want %d", got, want)
		}
		want++
	}
	if cap(q.items) > 2*next {
		t.Errorf("backing array grew to %d for %d items", cap(q.items), next)
	}
}
