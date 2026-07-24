package mcp

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPHandlerStreamableHandshake(t *testing.T) {
	s := &Server{
		RequireAuth:   false,
		AnonymousUser: &UserIdentity{Username: StdioUsername},
	}
	h := s.HTTPHandler()

	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("initialize status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type=%q", ct)
	}
	sid := rr.Header().Get("Mcp-Session-Id")
	if sid == "" {
		t.Fatal("missing Mcp-Session-Id")
	}
	if !strings.Contains(rr.Body.String(), "event: message") {
		t.Fatalf("expected SSE body, got %q", rr.Body.String())
	}

	notif := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(notif))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", sid)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("initialized status=%d body=%s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("oauth status=%d", rr.Code)
	}
	body, _ := io.ReadAll(rr.Body)
	if !bytes.Contains(body, []byte("Authorization URL is not configured")) {
		t.Fatalf("oauth body=%q", body)
	}
}
