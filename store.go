package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// store persists the guestbook + visitor count to a JSON file under $DATA_DIR
// (e.g. a mounted volume). When DATA_DIR is unset or not writable it's a no-op
// and the live state stays in memory only — so the app runs fine without a disk.
type store struct {
	mu   sync.Mutex
	path string // empty = disabled
}

type guestEntryDTO struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

type persisted struct {
	Visits  int             `json:"visits"`
	Entries []guestEntryDTO `json:"entries"`
}

func newStore() *store {
	dir := os.Getenv("DATA_DIR")
	if dir == "" {
		return &store{}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Println("DATA_DIR unusable, guestbook stays in-memory:", err)
		return &store{}
	}
	// Make sure we can actually write there (volumes can be read-only for a
	// non-root user); fall back to in-memory if not.
	probe := filepath.Join(dir, ".write-test")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		fmt.Println("DATA_DIR not writable, guestbook stays in-memory:", err)
		return &store{}
	}
	_ = os.Remove(probe)
	return &store{path: filepath.Join(dir, "guestbook.json")}
}

func (s *store) enabled() bool { return s.path != "" }

func (s *store) load() (int, []guestEntry) {
	if !s.enabled() {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if err != nil {
		return 0, nil
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return 0, nil
	}
	entries := make([]guestEntry, len(p.Entries))
	for i, e := range p.Entries {
		entries[i] = guestEntry{name: e.Name, text: e.Text}
	}
	return p.Visits, entries
}

// save writes the state atomically (temp file + rename).
func (s *store) save(visits int, entries []guestEntry) {
	if !s.enabled() {
		return
	}
	dtos := make([]guestEntryDTO, len(entries))
	for i, e := range entries {
		dtos[i] = guestEntryDTO{Name: e.name, Text: e.text}
	}
	b, err := json.Marshal(persisted{Visits: visits, Entries: dtos})
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}
