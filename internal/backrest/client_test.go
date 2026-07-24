package backrest

import "testing"

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
