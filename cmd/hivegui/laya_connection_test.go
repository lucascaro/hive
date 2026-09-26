package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLayaConnectionOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("path = %q, want /health", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("the connection test sent credentials")
		}
	}))
	defer srv.Close()
	if got := (&App{}).TestLayaConnection(srv.URL + "/"); got != "" {
		t.Errorf("TestLayaConnection = %q, want empty for a healthy server", got)
	}
}

func TestLayaConnectionReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	if got := (&App{}).TestLayaConnection(srv.URL); !strings.Contains(got, "503") {
		t.Errorf("TestLayaConnection = %q, want the 503 named", got)
	}
}

func TestLayaConnectionUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	if got := (&App{}).TestLayaConnection(url); got == "" {
		t.Error("TestLayaConnection reported a closed port as healthy")
	}
}
