package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"io"
	"log"
	"net/http"
	"strings"
)

func Handler(s *coord.Service, adminToken string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("POST /v1/workspaces", func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(bearer(r)), []byte(adminToken)) != 1 {
			respond(w, 401, map[string]string{"error": "admin bearer token required"})
			return
		}
		var a struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &a) {
			return
		}
		out, err := s.Create(r.Context(), a.Name)
		result(w, out, err)
	})
	mux.HandleFunc("POST /v1/workspaces/{id}/actions/{action}", func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r)
		if token == "" {
			respond(w, 401, map[string]string{"error": "workspace bearer token required"})
			return
		}
		var a coord.Arguments
		if !decode(w, r, &a) {
			return
		}
		out, err := s.Execute(r.Context(), r.PathValue("id"), token, r.PathValue("action"), a)
		result(w, out, err)
	})
	return mux
}
func bearer(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(value, "Bearer ")
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		respond(w, 400, map[string]string{"error": "invalid JSON request"})
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		respond(w, 400, map[string]string{"error": "one JSON object required"})
		return false
	}
	return true
}
func result(w http.ResponseWriter, out any, err error) {
	if err != nil {
		var e *coord.Error
		if errors.As(err, &e) {
			respond(w, e.Status, map[string]string{"error": e.Message})
			return
		}
		log.Printf("request failed: %v", err)
		respond(w, 500, map[string]string{"error": "internal server error"})
		return
	}
	respond(w, 200, out)
}
func respond(w http.ResponseWriter, status int, out any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(out)
}
