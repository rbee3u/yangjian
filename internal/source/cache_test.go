package source

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCachedTransport(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("X-Test", "cached")
		_, _ = w.Write([]byte("response-body"))
	}))
	defer server.Close()
	dir := t.TempDir()

	client := &http.Client{Transport: &cachedTransport{base: http.DefaultTransport, dir: dir}}
	for i := 0; i < 2; i++ {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "response-body" || response.StatusCode != http.StatusOK {
			t.Fatalf("body = %q, status = %d", body, response.StatusCode)
		}
		if response.Header.Get("X-Test") != "cached" {
			t.Fatalf("header = %v", response.Header)
		}
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}

	refreshed := &http.Client{Transport: &cachedTransport{base: http.DefaultTransport, dir: dir, refresh: true}}
	response, err := refreshed.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if requests != 2 {
		t.Fatalf("refresh did not bypass cache: %d requests", requests)
	}
}
