package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientProxyHandler(t *testing.T) {
	var gotPath, gotQuery, gotHost string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "next-ui")
	}))
	defer backend.Close()

	h := clientProxyHandler(backend.URL, "")
	req := httptest.NewRequest(http.MethodGet, "http://example.test/library/abc?sort=title", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "next-ui" {
		t.Fatalf("status=%d body=%q, want 200 next-ui", rec.Code, rec.Body.String())
	}
	if gotPath != "/library/abc" || gotQuery != "sort=title" || gotHost != "example.test" {
		t.Errorf("forwarded path=%q query=%q host=%q", gotPath, gotQuery, gotHost)
	}

	hBase := clientProxyHandler(backend.URL, "/abs")
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"/", "/abs"},
		{"/login", "/abs/login"},
	} {
		rec = httptest.NewRecorder()
		hBase.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.test"+tc.in, nil))
		if rec.Code != http.StatusOK || gotPath != tc.want {
			t.Errorf("in=%q: status=%d gotPath=%q, want %q", tc.in, rec.Code, gotPath, tc.want)
		}
	}

	// Closed backend returns 502 without leaking internal error detail.
	backend.Close()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.test/", nil))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("closed backend status=%d, want 502", rec.Code)
	}
}
