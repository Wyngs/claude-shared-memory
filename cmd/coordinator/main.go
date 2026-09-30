package main

import (
	"context"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"github.com/Wyngs/claude-shared-memory/internal/httpapi"
	"github.com/Wyngs/claude-shared-memory/internal/store"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	token := os.Getenv("ADMIN_TOKEN")
	if len(token) < 32 {
		log.Fatal("ADMIN_TOKEN must contain at least 32 characters")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var backend coord.Store
	if url := os.Getenv("DATABASE_URL"); url != "" {
		startup, done := context.WithTimeout(ctx, 15*time.Second)
		p, err := store.NewPostgres(startup, url)
		done()
		if err != nil {
			log.Fatalf("database startup: %v", err)
		}
		backend = p
	} else {
		if os.Getenv("STORE") != "memory" {
			log.Fatal("DATABASE_URL required; use STORE=memory explicitly for a disposable demo")
		}
		backend = store.NewMemory()
		log.Print("development memory store: state will be lost on restart")
	}
	defer backend.Close()
	address := os.Getenv("LISTEN_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	server := &http.Server{Addr: address, Handler: httpapi.Handler(coord.New(backend), token), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		server.Shutdown(shutdown)
	}()
	log.Printf("coordinator listening on %s", address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
