// Command export-proxy serves a single tar archive over HTTP with a path token.
// Used by PVCRestore export Jobs (no Python runtime).
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/Danpiel/backrest-operator/internal/logging"
)

func main() {
	log := logging.Setup("export-proxy", logging.FromEnv())

	token := os.Getenv("EXPORT_TOKEN")
	if token == "" {
		log.Error(fmt.Errorf("EXPORT_TOKEN is required"), "missing configuration")
		os.Exit(1)
	}
	file := envOr("EXPORT_FILE", "/work/archive.tar")
	addr := envOr("LISTEN", ":8080")
	oneshot := os.Getenv("EXPORT_ONESHOT") != "0"
	path := "/" + token + "/archive.tar"

	var done atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		remote := r.RemoteAddr
		if r.Method != http.MethodGet {
			log.Info("reject download", "remote", remote, "method", r.Method, "reason", "method not allowed")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if oneshot && done.Load() {
			log.Info("reject download", "remote", remote, "reason", "already consumed")
			http.Error(w, "gone", http.StatusGone)
			return
		}
		f, err := os.Open(file)
		if err != nil {
			log.Error(err, "open export file", "file", file, "remote", remote)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		st, err := f.Stat()
		if err != nil {
			log.Error(err, "stat export file", "file", file, "remote", remote)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Info("serving download", "remote", remote, "file", file, "bytes", st.Size(), "oneshot", oneshot)
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		w.WriteHeader(http.StatusOK)
		n, copyErr := io.Copy(w, f)
		if copyErr != nil {
			log.Error(copyErr, "stream export file", "remote", remote, "written", n)
		} else {
			log.Info("download finished", "remote", remote, "bytes", n)
		}
		done.Store(true)
		if oneshot {
			go func() {
				time.Sleep(200 * time.Millisecond)
				log.Info("oneshot complete, exiting")
				os.Exit(0)
			}()
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	log.Info("listening", "addr", addr, "path", "/<token>/archive.tar", "file", file, "oneshot", oneshot)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Error(err, "HTTP server stopped")
		os.Exit(1)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
