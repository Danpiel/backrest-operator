package filters_test

import (
	"os"
	"testing"

	"github.com/Danpiel/backrest-operator/internal/filters"
)

func TestNamespaceAllowedAll(t *testing.T) {
	os.Unsetenv("WATCH_NAMESPACES")
	if !filters.NamespaceAllowed("any") {
		t.Fatal("expected all allowed")
	}
}

func TestNamespaceAllowedList(t *testing.T) {
	t.Setenv("WATCH_NAMESPACES", "a,b")
	if !filters.NamespaceAllowed("a") || filters.NamespaceAllowed("c") {
		t.Fatal("allow-list failed")
	}
}

func TestLabelsMatch(t *testing.T) {
	t.Setenv("WATCH_LABEL_SELECTOR", "app=backrest")
	if !filters.LabelsMatch(map[string]string{"app": "backrest"}) {
		t.Fatal("expected match")
	}
	if filters.LabelsMatch(map[string]string{"app": "other"}) {
		t.Fatal("expected mismatch")
	}
}
