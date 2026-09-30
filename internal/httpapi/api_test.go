package httpapi_test

import (
	"bytes"
	"github.com/Wyngs/claude-shared-memory/internal/coord"
	"github.com/Wyngs/claude-shared-memory/internal/httpapi"
	"github.com/Wyngs/claude-shared-memory/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestBoundaries(t *testing.T) {
	h := httpapi.Handler(coord.New(store.NewMemory()), "administrator-token-at-least-32-chars")
	cases := []struct {
		body, token string
		status      int
	}{{`{"name":"test"}`, "", 401}, {`{"name":"test","unknown":1}`, "administrator-token-at-least-32-chars", 400}, {`{"name":"test"} {}`, "administrator-token-at-least-32-chars", 400}, {`{"name":"test"}`, "administrator-token-at-least-32-chars", 200}}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodPost, "/v1/workspaces", bytes.NewBufferString(c.body))
		r.Header.Set("Authorization", "Bearer "+c.token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.status {
			t.Fatalf("expected %d got %d: %s", c.status, w.Code, w.Body.String())
		}
	}
}
