package filters

import (
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WatchedNamespaces returns allow-list or nil for all namespaces.
func WatchedNamespaces() []string {
	raw := strings.TrimSpace(os.Getenv("WATCH_NAMESPACES"))
	if raw == "" || raw == "*" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// NamespaceAllowed reports whether ns is in the allow-list.
func NamespaceAllowed(ns string) bool {
	allow := WatchedNamespaces()
	if allow == nil {
		return true
	}
	for _, a := range allow {
		if a == ns {
			return true
		}
	}
	return false
}

// LabelSelectorMap parses WATCH_LABEL_SELECTOR k=v,k2=v2.
func LabelSelectorMap() map[string]string {
	raw := strings.TrimSpace(os.Getenv("WATCH_LABEL_SELECTOR"))
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" || !strings.Contains(p, "=") {
			continue
		}
		kv := strings.SplitN(p, "=", 2)
		out[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
	}
	return out
}

// LabelsMatch returns true if required selector is satisfied by labels.
func LabelsMatch(labels map[string]string) bool {
	sel := LabelSelectorMap()
	if len(sel) == 0 {
		return true
	}
	if labels == nil {
		return false
	}
	for k, v := range sel {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// ObjectAllowed combines namespace + label filters for an object.
func ObjectAllowed(ns string, labels map[string]string) bool {
	return NamespaceAllowed(ns) && LabelsMatch(labels)
}

// CacheNamespaces returns namespace list for manager cache (nil = all).
func CacheNamespaces() []string {
	return WatchedNamespaces()
}

// LabelSelectorAsString returns the env selector for ListOptions.
func LabelSelectorAsString() string {
	return strings.TrimSpace(os.Getenv("WATCH_LABEL_SELECTOR"))
}

// MatchLabels helper for metav1.
func MatchLabels() *metav1.LabelSelector {
	m := LabelSelectorMap()
	if len(m) == 0 {
		return nil
	}
	return &metav1.LabelSelector{MatchLabels: m}
}
