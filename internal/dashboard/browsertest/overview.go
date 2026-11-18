package main

import (
	"bufio"
	"io"
	"sync"
	"time"

	"github.com/taco3064/gh-nibfin/internal/snapshot"
)

// Test-owned stdin advances observations without a browser-facing testing API.
type overviewSequence struct {
	mu     sync.Mutex
	values []snapshot.Overview
	index  int
}

func (s *overviewSequence) CollectOverview() snapshot.Overview {
	s.mu.Lock()
	defer s.mu.Unlock()
	value := s.values[s.index]
	now := time.Now().UTC()
	value.ObservedAt, value.ExpiresAt = now, now.Add(30*time.Second)
	value.Runners.Items = append([]snapshot.LocalRunner(nil), value.Runners.Items...)
	for index := range value.Runners.Items {
		runner := &value.Runners.Items[index]
		runner.Busy.ObservedAt, runner.Online.ObservedAt = now, now
		runner.Association.ObservedAt = now
	}
	return value
}

func readOverviewCommands(input io.Reader, sequence *overviewSequence) {
	scanner := bufio.NewScanner(input)
	for scanner.Scan() {
		if scanner.Text() != "next" {
			continue
		}
		sequence.mu.Lock()
		if sequence.index+1 < len(sequence.values) {
			sequence.index++
		}
		sequence.mu.Unlock()
	}
}
