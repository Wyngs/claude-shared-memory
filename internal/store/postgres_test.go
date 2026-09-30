package store_test

import (
	"context"
	"errors"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"github.com/Wyngs/claude-shared-memory/internal/store"
	"os"
	"testing"
)

func TestPostgresDurabilityAndRollback(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	first, err := store.NewPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	service := coord.New(first)
	workspace, err := service.Create(ctx, "restart test")
	if err != nil {
		t.Fatal(err)
	}
	id, token := workspace["workspace_id"], workspace["workspace_token"]
	joined, err := service.Execute(ctx, id, token, "join_workspace", coord.Arguments{Name: "investigator"})
	if err != nil {
		t.Fatal(err)
	}
	j := joined.(map[string]any)
	a := coord.Arguments{AgentID: j["agent"].(coord.Agent).ID, AgentToken: j["agent_token"].(string), Summary: "persists across reconnect"}
	if _, err = service.Execute(ctx, id, token, "publish_finding", a); err != nil {
		t.Fatal(err)
	}
	// Store contract guarantees a failing mutation cannot leak partial state.
	rollback := errors.New("abort transaction")
	err = first.Update(ctx, id, coord.Hash(token), func(w *coord.Workspace) error { w.Name = "should not persist"; return rollback })
	if !errors.Is(err, rollback) {
		t.Fatal("expected rollback error")
	}
	first.Close()
	second, err := store.NewPostgres(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	out, err := coord.New(second).Execute(ctx, id, token, "get_workspace_context", a)
	if err != nil {
		t.Fatal(err)
	}
	m := out.(map[string]any)
	w := m["workspace"].(coord.Workspace)
	if w.Name != "restart test" || len(w.Findings) != 1 || m["cursor"].(int) != 2 {
		t.Fatal("state or cursor did not survive store reconnect")
	}
}
