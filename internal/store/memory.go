package store

import (
	"context"
	"encoding/json"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"sync"
)

type record struct {
	State coord.Workspace
	Hash  string
}

// Memory is a development store. It deliberately uses the same transaction contract as PostgreSQL.
type Memory struct {
	mu      sync.Mutex
	records map[string]record
}

func NewMemory() *Memory { return &Memory{records: map[string]record{}} }
func (m *Memory) Close() {}
func (m *Memory) Create(_ context.Context, w coord.Workspace, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records[w.ID] = record{w, hash}
	return nil
}
func (m *Memory) Update(_ context.Context, id, hash string, fn func(*coord.Workspace) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok || hash != r.Hash {
		return &coord.Error{Status: 404, Message: "workspace not found or unauthorized"}
	}
	data, err := json.Marshal(r.State)
	if err != nil {
		return err
	}
	var copy coord.Workspace
	if err = json.Unmarshal(data, &copy); err != nil {
		return err
	}
	if err = fn(&copy); err != nil {
		return err
	}
	r.State = copy
	m.records[id] = r
	return nil
}
