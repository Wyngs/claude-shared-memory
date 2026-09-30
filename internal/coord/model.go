package coord

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string              { return e.Message }
func fail(status int, message string) error { return &Error{status, message} }
func ID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func Hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type Agent struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	LastSeen  time.Time `json:"last_seen"`
	TokenHash string    `json:"token_hash,omitempty"`
}
type Lease struct {
	AgentID   string    `json:"agent_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Lease  *Lease `json:"lease,omitempty"`
	Result string `json:"result,omitempty"`
}
type Finding struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Summary   string    `json:"summary"`
	Evidence  string    `json:"evidence"`
	CreatedAt time.Time `json:"created_at"`
}
type Event struct {
	Sequence int64     `json:"sequence"`
	Kind     string    `json:"kind"`
	AgentID  string    `json:"agent_id,omitempty"`
	EntityID string    `json:"entity_id,omitempty"`
	At       time.Time `json:"at"`
}
type Workspace struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Agents    map[string]Agent `json:"agents"`
	Tasks     map[string]Task  `json:"tasks"`
	Findings  []Finding        `json:"findings"`
	Resources map[string]Lease `json:"resources"`
	Events    []Event          `json:"events"`
}
type Store interface {
	Create(context.Context, Workspace, string) error
	// Update must serialize updates, authenticate the workspace token and roll back on error.
	Update(context.Context, string, string, func(*Workspace) error) error
	Close()
}
type Arguments struct {
	Name         string `json:"name,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	AgentToken   string `json:"agent_token,omitempty"`
	Title        string `json:"title,omitempty"`
	TaskID       string `json:"task_id,omitempty"`
	LeaseToken   string `json:"lease_token,omitempty"`
	LeaseSeconds int    `json:"lease_seconds,omitempty"`
	Result       string `json:"result,omitempty"`
	Summary      string `json:"summary,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
	Resource     string `json:"resource,omitempty"`
	After        int64  `json:"after,omitempty"`
}
type Service struct {
	Store Store
	Now   func() time.Time
}

func New(store Store) *Service { return &Service{store, func() time.Time { return time.Now().UTC() }} }
func (s *Service) Create(ctx context.Context, name string) (map[string]string, error) {
	if len(name) == 0 || len(name) > 200 {
		return nil, fail(400, "name must contain 1 to 200 bytes")
	}
	w := Workspace{ID: ID(), Name: name, Agents: map[string]Agent{}, Tasks: map[string]Task{}, Resources: map[string]Lease{}, Findings: []Finding{}, Events: []Event{}}
	token := ID()
	err := s.Store.Create(ctx, w, Hash(token))
	return map[string]string{"workspace_id": w.ID, "workspace_token": token}, err
}
func event(w *Workspace, now time.Time, kind, agent, entity string) {
	w.Events = append(w.Events, Event{int64(len(w.Events) + 1), kind, agent, entity, now})
}
func duration(seconds int) (time.Duration, error) {
	if seconds == 0 {
		seconds = 120
	}
	if seconds < 15 || seconds > 3600 {
		return 0, fail(400, "lease_seconds must be between 15 and 3600")
	}
	return time.Duration(seconds) * time.Second, nil
}
func checkLease(l *Lease, a Arguments, now time.Time) error {
	if l == nil || !now.Before(l.ExpiresAt) {
		return fail(409, "lease expired or unclaimed")
	}
	if l.AgentID != a.AgentID || l.Token != a.LeaseToken {
		return fail(409, "lease ownership changed")
	}
	return nil
}
func public(w Workspace, now time.Time) Workspace {
	agents := map[string]Agent{}
	for id, a := range w.Agents {
		a.TokenHash = ""
		agents[id] = a
	}
	w.Agents = agents
	tasks := map[string]Task{}
	for id, t := range w.Tasks {
		if t.Lease != nil {
			l := *t.Lease
			l.Token = ""
			t.Lease = &l
			if !now.Before(l.ExpiresAt) && t.Status == "claimed" {
				t.Status = "available"
			}
		}
		tasks[id] = t
	}
	w.Tasks = tasks
	resources := map[string]Lease{}
	for id, l := range w.Resources {
		if now.Before(l.ExpiresAt) {
			l.Token = ""
			resources[id] = l
		}
	}
	w.Resources = resources
	return w
}
func (s *Service) Execute(ctx context.Context, id, token, action string, a Arguments) (any, error) {
	var output any
	err := s.Store.Update(ctx, id, Hash(token), func(w *Workspace) error {
		now := s.Now()
		if action == "join_workspace" {
			if len(a.Name) == 0 || len(a.Name) > 200 {
				return fail(400, "name must contain 1 to 200 bytes")
			}
			secret := ID()
			agent := Agent{ID(), a.Name, now, Hash(secret)}
			w.Agents[agent.ID] = agent
			event(w, now, "agent.joined", agent.ID, agent.ID)
			agent.TokenHash = ""
			output = map[string]any{"agent": agent, "agent_token": secret}
			return nil
		}
		agent, ok := w.Agents[a.AgentID]
		if !ok || agent.TokenHash != Hash(a.AgentToken) {
			return fail(403, "valid agent_id and agent_token required")
		}
		agent.LastSeen = now
		w.Agents[agent.ID] = agent
		switch action {
		case "heartbeat":
			output = map[string]any{"last_seen": now}
		case "get_workspace_context":
			snapshot := public(*w, now)
			snapshot.Events = nil
			output = map[string]any{"workspace": snapshot, "cursor": len(w.Events)}
		case "poll_events":
			if a.After < 0 || a.After > int64(len(w.Events)) {
				return fail(400, "invalid event cursor")
			}
			end := a.After + 100
			if end > int64(len(w.Events)) {
				end = int64(len(w.Events))
			}
			output = map[string]any{"events": w.Events[a.After:end], "cursor": end, "has_more": end < int64(len(w.Events))}
		case "announce_work":
			if len(a.Title) == 0 || len(a.Title) > 500 {
				return fail(400, "title must contain 1 to 500 bytes")
			}
			task := Task{ID: ID(), Title: a.Title, Status: "available"}
			w.Tasks[task.ID] = task
			event(w, now, "task.created", agent.ID, task.ID)
			output = task
		case "claim_task", "renew_task", "complete_task":
			task, ok := w.Tasks[a.TaskID]
			if !ok {
				return fail(404, "task not found")
			}
			if task.Status == "completed" {
				return fail(409, "task already completed")
			}
			if action == "claim_task" {
				if task.Lease != nil && now.Before(task.Lease.ExpiresAt) {
					return fail(409, "task already claimed")
				}
				ttl, err := duration(a.LeaseSeconds)
				if err != nil {
					return err
				}
				task.Lease = &Lease{agent.ID, ID(), now.Add(ttl)}
				task.Status = "claimed"
			} else {
				if err := checkLease(task.Lease, a, now); err != nil {
					return err
				}
				if action == "renew_task" {
					ttl, err := duration(a.LeaseSeconds)
					if err != nil {
						return err
					}
					task.Lease.ExpiresAt = now.Add(ttl)
				} else {
					if len(a.Result) == 0 || len(a.Result) > 10000 {
						return fail(400, "result must contain 1 to 10000 bytes")
					}
					task.Status = "completed"
					task.Result = a.Result
					task.Lease = nil
				}
			}
			w.Tasks[task.ID] = task
			event(w, now, action, agent.ID, task.ID)
			output = task
		case "publish_finding":
			if len(a.Summary) == 0 || len(a.Summary) > 2000 || len(a.Evidence) > 10000 {
				return fail(400, "summary required (max 2000 bytes); evidence max 10000 bytes")
			}
			f := Finding{ID(), agent.ID, a.Summary, a.Evidence, now}
			w.Findings = append(w.Findings, f)
			event(w, now, "finding.published", agent.ID, f.ID)
			output = f
		case "claim_resource", "renew_resource", "release_resource":
			if len(a.Resource) == 0 || len(a.Resource) > 500 {
				return fail(400, "resource must contain 1 to 500 bytes")
			}
			l, exists := w.Resources[a.Resource]
			if action == "claim_resource" {
				if exists && now.Before(l.ExpiresAt) {
					return fail(409, "resource already claimed")
				}
				ttl, err := duration(a.LeaseSeconds)
				if err != nil {
					return err
				}
				l = Lease{agent.ID, ID(), now.Add(ttl)}
				w.Resources[a.Resource] = l
				output = l
			} else {
				if !exists {
					return fail(409, "resource unclaimed")
				}
				if err := checkLease(&l, a, now); err != nil {
					return err
				}
				if action == "renew_resource" {
					ttl, err := duration(a.LeaseSeconds)
					if err != nil {
						return err
					}
					l.ExpiresAt = now.Add(ttl)
					w.Resources[a.Resource] = l
					output = l
				} else {
					delete(w.Resources, a.Resource)
					output = map[string]bool{"released": true}
				}
			}
			event(w, now, action, agent.ID, a.Resource)
		default:
			return fail(404, fmt.Sprintf("unknown action: %s", action))
		}
		return nil
	})
	return output, err
}
