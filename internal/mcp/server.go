package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Danpiel/backrest-operator/internal/logging"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type callParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type Server struct {
	Auth           *Auth
	Tools          *Tools
	RequireAuth    bool
	AnonymousUser  *UserIdentity
}

func NewServer(cfg *rest.Config, onDeny func(tool string), requireAuth bool) (*Server, error) {
	auth, err := NewAuth(cfg, onDeny)
	if err != nil {
		return nil, err
	}
	return &Server{
		Auth:        auth,
		Tools:       NewTools(cfg),
		RequireAuth: requireAuth,
		AnonymousUser: &UserIdentity{
			Username: StdioUsername,
			Groups:   []string{"system:authenticated", "backrest:mcp-open"},
		},
	}, nil
}

func (s *Server) HandleRPC(ctx context.Context, user *UserIdentity, body []byte) (resp []byte, httpStatus int, method string) {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return rpcError(nil, -32700, "parse error"), http.StatusBadRequest, ""
	}
	method = req.Method
	log := ctrl.LoggerFrom(ctx).WithName("mcp")
	// Protocol chatter stays at debug — do not spam info on every Cursor poll.
	switch {
	case strings.HasPrefix(method, "notifications/"), method == "initialize", method == "ping", method == "tools/list":
		log.V(1).Info("rpc", "method", method, "bytes", len(body))
	default:
		log.V(1).Info("rpc", "method", method, "bytes", len(body), "body", logging.BodySummary(body, 240))
	}
	// JSON-RPC notifications have no id; Streamable HTTP expects HTTP 202 + empty body.
	if strings.HasPrefix(req.Method, "notifications/") {
		return nil, http.StatusAccepted, method
	}
	switch req.Method {
	case "initialize":
		return rpcResult(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "backrest-mcp", "version": "0.2.0"},
		}), http.StatusOK, method
	case "ping":
		return rpcResult(req.ID, map[string]interface{}{}), http.StatusOK, method
	case "tools/list":
		return rpcResult(req.ID, map[string]interface{}{"tools": ToolSchemas()}), http.StatusOK, method
	case "tools/call":
		var params callParams
		_ = json.Unmarshal(req.Params, &params)
		ns := "default"
		if params.Arguments != nil {
			if v, ok := params.Arguments["namespace"].(string); ok && v != "" {
				ns = v
			}
		}
		allow := false
		if params.Arguments != nil {
			if v, ok := params.Arguments["allow_destructive"].(bool); ok {
				allow = v
			}
		}
		userName := ""
		if user != nil {
			userName = user.Username
		}
		tlog := log.WithValues("tool", params.Name, "namespace", ns, "user", userName)
		if !s.Auth.AuthorizeTool(ctx, user, params.Name, ns, allow, params.Arguments) {
			tlog.Info("tool denied")
			return rpcError(req.ID, 403, fmt.Sprintf("forbidden: %s", params.Name)), http.StatusForbidden, method
		}
		tlog.V(1).Info("tool starting", "args", logging.BodySummary(mustJSON(params.Arguments), 240))
		started := time.Now()
		result, err := s.Tools.Call(ctx, user, params.Name, params.Arguments)
		elapsed := time.Since(started).Round(time.Millisecond)
		if err != nil {
			tlog.Error(err, "tool failed", "duration", elapsed)
			if strings.Contains(err.Error(), "allow_destructive") {
				return rpcError(req.ID, 403, err.Error()), http.StatusForbidden, method
			}
			return rpcError(req.ID, 500, err.Error()), http.StatusInternalServerError, method
		}
		text, _ := json.Marshal(result)
		tlog.Info("tool completed", "duration", elapsed)
		tlog.V(1).Info("tool result", "bytes", len(text), "body", logging.BodySummary(text, 240))
		return rpcResult(req.ID, map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": string(text)}},
		}), http.StatusOK, method
	default:
		return rpcError(req.ID, -32601, "method not found"), http.StatusNotFound, method
	}
}

func mustJSON(v interface{}) []byte {
	if v == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func rpcResult(id interface{}, result interface{}) []byte {
	b, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result})
	return b
}

func rpcError(id interface{}, code int, message string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error":   map[string]interface{}{"code": code, "message": message},
	})
	return b
}

func (s *Server) HTTPHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// Match kubernetes-mcp-server: tell Cursor/Codex OAuth is not configured (not a generic 404).
	oauthNotConfigured := func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Authorization URL is not configured", http.StatusNotFound)
	}
	mux.HandleFunc("/.well-known/oauth-authorization-server", oauthNotConfigured)
	mux.HandleFunc("/.well-known/oauth-authorization-server/", oauthNotConfigured)
	mux.HandleFunc("/.well-known/oauth-protected-resource", oauthNotConfigured)
	mux.HandleFunc("/.well-known/oauth-protected-resource/", oauthNotConfigured)
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			// ok
		case http.MethodGet, http.MethodDelete:
			w.Header().Set("Allow", "POST")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		default:
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		user, err := s.resolveHTTPUser(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp, status, method := s.HandleRPC(r.Context(), user, body)
		if status == http.StatusAccepted {
			w.WriteHeader(status)
			return
		}
		if method == "initialize" && r.Header.Get("Mcp-Session-Id") == "" {
			w.Header().Set("Mcp-Session-Id", newSessionID())
		} else if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
			w.Header().Set("Mcp-Session-Id", sid)
		}
		accept := r.Header.Get("Accept")
		if strings.Contains(accept, "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache, no-transform")
			w.WriteHeader(status)
			_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", resp)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(resp)
	})
	return mux
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(fmt.Sprintf("%d", os.Getpid())))
	}
	return strings.ToUpper(hex.EncodeToString(b[:]))
}

func (s *Server) resolveHTTPUser(r *http.Request) (*UserIdentity, error) {
	auth := r.Header.Get("Authorization")
	hasBearer := strings.HasPrefix(strings.ToLower(auth), "bearer ")
	if !s.RequireAuth {
		if hasBearer {
			token := strings.TrimSpace(auth[7:])
			if token != "" {
				if user, err := s.Auth.ReviewToken(r.Context(), token); err == nil {
					return user, nil
				}
			}
		}
		return s.AnonymousUser, nil
	}
	if !hasBearer {
		return nil, fmt.Errorf("Bearer Kubernetes token required")
	}
	token := strings.TrimSpace(auth[7:])
	return s.Auth.ReviewToken(r.Context(), token)
}

func envBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}

func (s *Server) RunStdio(ctx context.Context) error {
	user := &UserIdentity{Username: StdioUsername, Groups: []string{"system:authenticated"}}
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		resp, status, _ := s.HandleRPC(ctx, user, []byte(line))
		if status == http.StatusAccepted || len(resp) == 0 {
			continue
		}
		_, _ = out.Write(resp)
		_ = out.WriteByte('\n')
		_ = out.Flush()
	}
	return sc.Err()
}
