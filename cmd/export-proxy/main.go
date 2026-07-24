// Command export-proxy serves a single tar archive over HTTP with a path token.
// Used by PVCRestore export Jobs (no Python runtime).
package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

func main() {
	token := os.Getenv("EXPORT_TOKEN")
	if token == "" {
		log.Fatal("EXPORT_TOKEN required")
	}
	file := envOr("EXPORT_FILE", "/work/archive.tar")
	addr := envOr("LISTEN", ":8080")
	oneshot := os.Getenv("EXPORT_ONESHOT") != "0"
	path := "/" + token + "/archive.tar"

	var done atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if oneshot && done.Load() {
			http.Error(w, "gone", http.StatusGone)
			return
		}
		f, err := os.Open(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, f)
		done.Store(true)
		if oneshot {
			go func() {
				time.Sleep(200 * time.Millisecond)
				os.Exit(0)
			}()
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("export-proxy listening on %s path=%s", addr, path)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
