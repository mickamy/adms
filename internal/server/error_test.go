package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mickamy/adms/internal/server"
)

func TestWriteProblem(t *testing.T) {
	t.Parallel()

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users?id=eq.1", nil)
	rec := httptest.NewRecorder()

	server.WriteProblem(rec, r, http.StatusBadRequest,
		"unknown-column", "Unknown column", `column "foo" does not exist`)

	res := rec.Result()
	defer func() { _ = res.Body.Close() }()

	if got, want := res.StatusCode, http.StatusBadRequest; got != want {
		t.Errorf("StatusCode = %d, want %d", got, want)
	}

	if got, want := res.Header.Get("Content-Type"), "application/problem+json"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}

	var got server.Problem
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := server.Problem{
		Type:     server.ProblemTypePrefix + "unknown-column",
		Title:    "Unknown column",
		Status:   http.StatusBadRequest,
		Detail:   `column "foo" does not exist`,
		Instance: "/users?id=eq.1",
	}

	if got != want {
		t.Errorf("body = %+v, want %+v", got, want)
	}
}
