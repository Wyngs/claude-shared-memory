package coord_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"github.com/Wyngs/claude-shared-memory/internal/store"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func exercise(t *testing.T, backend coord.Store) {
	t.Helper()
	ctx := context.Background()
	s := coord.New(backend)
	now := time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	created, err := s.Create(ctx, "latency incident")
	if err != nil {
		t.Fatal(err)
	}
	id, token := created["workspace_id"], created["workspace_token"]
	run := func(action string, a coord.Arguments) any {
		t.Helper()
		out, err := s.Execute(ctx, id, token, action, a)
		if err != nil {
			t.Fatalf("%s: %v", action, err)
		}
		return out
	}
	join := func(name string) coord.Arguments {
		out := run("join_workspace", coord.Arguments{Name: name}).(map[string]any)
		return coord.Arguments{AgentID: out["agent"].(coord.Agent).ID, AgentToken: out["agent_token"].(string)}
	}
	a, b := join("logs"), join("configuration")
	task := run("announce_work", coord.Arguments{AgentID: a.AgentID, AgentToken: a.AgentToken, Title: "check connection recycling"}).(coord.Task)
	if _, err := s.Execute(ctx, id, "wrong", "heartbeat", a); err == nil {
		t.Fatal("bad workspace token accepted")
	}
	forged := a
	forged.AgentToken = b.AgentToken
	if _, err := s.Execute(ctx, id, token, "heartbeat", forged); err == nil {
		t.Fatal("impersonation accepted")
	}
	var winners atomic.Int32
	var claimed coord.Task
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := a
			if i%2 == 0 {
				args = b
			}
			args.TaskID = task.ID
			args.LeaseSeconds = 15
			out, err := s.Execute(ctx, id, token, "claim_task", args)
			if err == nil {
				winners.Add(1)
				mu.Lock()
				claimed = out.(coord.Task)
				mu.Unlock()
			} else {
				var e *coord.Error
				if !errors.As(err, &e) || e.Status != 409 {
					t.Errorf("unexpected claim error: %v", err)
				}
			}
		}(i)
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("expected exactly one owner, got %d", winners.Load())
	}
	owner := a
	other := b
	if claimed.Lease.AgentID == b.AgentID {
		owner, other = b, a
	}
	owner.TaskID = task.ID
	owner.LeaseToken = claimed.Lease.Token
	snapshot := run("get_workspace_context", other).(map[string]any)["workspace"].(coord.Workspace)
	if snapshot.Agents[owner.AgentID].TokenHash != "" || snapshot.Tasks[task.ID].Lease.Token != "" {
		t.Fatal("context leaked credentials")
	}
	// A stale session must not complete after expiry, even before another session reclaims.
	now = now.Add(16 * time.Second)
	owner.Result = "done"
	if _, err := s.Execute(ctx, id, token, "complete_task", owner); err == nil {
		t.Fatal("expired owner completed task")
	}
	other.TaskID = task.ID
	reclaimed := run("claim_task", other).(coord.Task)
	other.LeaseToken = reclaimed.Lease.Token
	if reclaimed.Lease.Token == owner.LeaseToken {
		t.Fatal("ownership token reused")
	}
	if _, err := s.Execute(ctx, id, token, "renew_task", owner); err == nil {
		t.Fatal("stale owner renewed task")
	}
	other.Result = "idle connections retain routing; recycle connections"
	run("complete_task", other)
	owner.Summary = "load test reproduces connection stickiness"
	owner.Evidence = "p99=800ms after scale-out"
	run("publish_finding", owner)
	other.Resource = "repo:router.go"
	l := run("claim_resource", other).(coord.Lease)
	owner.Resource = other.Resource
	if _, err := s.Execute(ctx, id, token, "claim_resource", owner); err == nil {
		t.Fatal("overlapping resource claim accepted")
	}
	owner.LeaseToken = l.Token
	if _, err := s.Execute(ctx, id, token, "release_resource", owner); err == nil {
		t.Fatal("wrong agent released resource")
	}
	other.LeaseToken = l.Token
	run("renew_resource", other)
	run("release_resource", other)
	events := run("poll_events", a).(map[string]any)["events"].([]coord.Event)
	for i, e := range events {
		if e.Sequence != int64(i+1) {
			t.Fatal("non-contiguous event cursor")
		}
	}
	a.After = int64(len(events))
	tail := run("poll_events", a).(map[string]any)
	if len(tail["events"].([]coord.Event)) != 0 {
		t.Fatal("events duplicated after cursor")
	}
	snapshot = run("get_workspace_context", a).(map[string]any)["workspace"].(coord.Workspace)
	if len(snapshot.Findings) != 1 || snapshot.Tasks[task.ID].Status != "completed" {
		t.Fatal("shared results missing")
	}
	// An invalid operation must not persist any mutation.
	a.TaskID = task.ID
	if _, err := s.Execute(ctx, id, token, "claim_task", a); err == nil {
		t.Fatal("completed task reclaimed")
	}
}
func TestTwoSessions(t *testing.T) { exercise(t, store.NewMemory()) }
func TestPostgresCoordination(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	p, err := store.NewPostgres(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	exercise(t, p)
}
func TestEventPagination(t *testing.T) {
	s := coord.New(store.NewMemory())
	ctx := context.Background()
	w, _ := s.Create(ctx, "pagination")
	call := func(action string, a coord.Arguments) any {
		t.Helper()
		v, e := s.Execute(ctx, w["workspace_id"], w["workspace_token"], action, a)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	j := call("join_workspace", coord.Arguments{Name: "a"}).(map[string]any)
	a := coord.Arguments{AgentID: j["agent"].(coord.Agent).ID, AgentToken: j["agent_token"].(string)}
	for i := 0; i < 110; i++ {
		a.Summary = fmt.Sprint(i)
		call("publish_finding", a)
	}
	p := call("poll_events", a).(map[string]any)
	if !p["has_more"].(bool) || p["cursor"].(int64) != 100 {
		t.Fatal("first page invalid")
	}
	a.After = 100
	p = call("poll_events", a).(map[string]any)
	if p["has_more"].(bool) || len(p["events"].([]coord.Event)) != 11 {
		t.Fatal("tail page invalid")
	}
}
