package requests

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoHTTPRequest_ReusesClientAcrossManyCalls(t *testing.T) {
	var hits int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	for i := 0; i < 32; i++ {
		if err := DoHTTPRequest(ts.URL, http.MethodGet, map[string]string{"Accept": "text/plain"}, "", http.StatusOK); err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	if hits != 32 {
		t.Fatalf("expected 32 hits, got %d", hits)
	}
}

func TestDoHTTPRequestWithTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	err := DoHTTPRequestWithTimeout(server.URL, http.MethodGet, nil, "", http.StatusOK, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}
