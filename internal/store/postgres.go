package store

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Postgres struct{ pool *pgxpool.Pool }

func NewPostgres(ctx context.Context, url string) (*Postgres, error) {
	p, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if _, err = p.Exec(ctx, `CREATE TABLE IF NOT EXISTS workspaces (id text PRIMARY KEY, token_hash text NOT NULL, state jsonb NOT NULL)`); err != nil {
		p.Close()
		return nil, err
	}
	return &Postgres{p}, nil
}
func (p *Postgres) Close() { p.pool.Close() }
func (p *Postgres) Create(ctx context.Context, w coord.Workspace, hash string) error {
	b, err := json.Marshal(w)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, "INSERT INTO workspaces(id,token_hash,state) VALUES($1,$2,$3)", w.ID, hash, b)
	return err
}
func (p *Postgres) Update(ctx context.Context, id, hash string, fn func(*coord.Workspace) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var data []byte
	err = tx.QueryRow(ctx, "SELECT state FROM workspaces WHERE id=$1 AND token_hash=$2 FOR UPDATE", id, hash).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return &coord.Error{Status: 404, Message: "workspace not found or unauthorized"}
	}
	if err != nil {
		return err
	}
	var w coord.Workspace
	if err = json.Unmarshal(data, &w); err != nil {
		return err
	}
	if err = fn(&w); err != nil {
		return err
	}
	data, err = json.Marshal(w)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE workspaces SET state=$2 WHERE id=$1", id, data); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
