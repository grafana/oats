package remote

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func TestEndpointSearchQueryEscaping(t *testing.T) {
	const query = `{service_name="foo+bar"} |= "a&b"`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("query"); got != query {
			t.Errorf("query = %q, want %q", got, query)
		}
		if r.URL.Path == "/pyroscope/render" {
			if got := r.URL.Query().Get("from"); got != "now-1m" {
				t.Errorf("from = %q, want %q", got, "now-1m")
			}
		}
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &Endpoint{
		host: parsed.Hostname(),
		ports: PortsConfig{
			LokiHTTPPort:      port,
			PyroscopeHTTPPort: port,
		},
	}

	if _, err := endpoint.SearchLoki(query); err != nil {
		t.Fatalf("SearchLoki: %v", err)
	}
	if _, err := endpoint.SearchPyroscope(query); err != nil {
		t.Fatalf("SearchPyroscope: %v", err)
	}
}
