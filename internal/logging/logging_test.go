package logging

import (
	"strings"
	"testing"
)

func TestTruncate(t *testing.T) {
	if Truncate("abc", 10) != "abc" {
		t.Fatal("short")
	}
	got := Truncate("abcdefghijklmnopqrstuvwxyz", 10)
	if !strings.HasPrefix(got, "abcdefghij") || !strings.Contains(got, "26B") {
		t.Fatalf("got %q", got)
	}
}

func TestRedactURL(t *testing.T) {
	in := "https://backup.prq-infra.net/download/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ4In0.sig/"
	got := RedactURL(in)
	want := "https://backup.prq-infra.net/download/<token>/"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBodySummary(t *testing.T) {
	got := BodySummary([]byte(`{"operationId":"18","status":"ok"}`), 100)
	if !strings.Contains(got, "operationId") || !strings.Contains(got, "B") {
		t.Fatalf("got %q", got)
	}
}
