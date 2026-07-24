package backrest

import (
	"encoding/base64"
	"testing"
)

func TestAbsoluteDownloadURL(t *testing.T) {
	got := AbsoluteDownloadURL("https://backup.prq-infra.net", "./download/abc/")
	want := "https://backup.prq-infra.net/download/abc/"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = AbsoluteDownloadURL("https://backup.prq-infra.net/", "/download/abc/")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestExpiryFromDownloadRelativeURL(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x","exp":1785003002}`))
	rel := "./download/aaa." + payload + ".sig/"
	exp, ok := ExpiryFromDownloadRelativeURL(rel)
	if !ok {
		t.Fatal("expected ok")
	}
	if exp.Unix() != 1785003002 {
		t.Fatalf("exp=%d", exp.Unix())
	}
}
