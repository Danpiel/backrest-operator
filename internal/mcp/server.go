package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

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
	Auth  *Auth
	Tools *Tools
}

func NewServer(cfg *rest.Config, onDeny func(tool string)) (*Server, error) {
	auth, err := NewAuth(cfg, onDeny)
	if err != nil {
		return nil, err
	}
	return &Server{Auth: auth, Tools: NewTools(cfg)}, nil
}

func (s *Server) HandleRPC(ctx context.Context, user *UserIdentity, body []byte) (resp []byte, httpStatus int) {
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return rpcError(nil, -32700, "parse error"), http.StatusBadRequest
	}
	switch req.Method {
	case "initialize":
		return rpcResult(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "backrest-mcp", "version": "0.2.0"},
		}), http.StatusOK
	case "tools/list":
		return rpcResult(req.ID, map[string]interface{}{"tools": ToolSchemas()}), http.StatusOK
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
		log := ctrl.LoggerFrom(ctx).WithName("mcp").WithValues("tool", params.Name, "namespace", ns, "user", userName)
		if !s.Auth.AuthorizeTool(ctx, user, params.Name, ns, allow, params.Arguments) {
			log.Info("tool denied")
			return rpcError(req.ID, 403, fmt.Sprintf("forbidden: %s", params.Name)), http.StatusForbidden
		}
		log.Info("tool call")
		result, err := s.Tools.Call(ctx, user, params.Name, params.Arguments)
		if err != nil {
			log.Error(err, "tool failed")
			if strings.Contains(err.Error(), "allow_destructive") {
				return rpcError(req.ID, 403, err.Error()), http.StatusForbidden
			}
			return rpcError(req.ID, 500, err.Error()), http.StatusInternalServerError
		}
		log.Info("tool ok")
		text, _ := json.Marshal(result)
		return rpcResult(req.ID, map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": string(text)}},
		}), http.StatusOK
	default:
		return rpcError(req.ID, -32601, "method not found"), http.StatusNotFound
	}
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
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
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
		resp, status := s.HandleRPC(r.Context(), user, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(resp)
	})
	return mux
}

func (s *Server) resolveHTTPUser(r *http.Request) (*UserIdentity, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return nil, fmt.Errorf("Bearer Kubernetes token required")
	}
	token := strings.TrimSpace(auth[7:])
	return s.Auth.ReviewToken(r.Context(), token)
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
		resp, _ := s.HandleRPC(ctx, user, []byte(line))
		_, _ = out.Write(resp)
		_ = out.WriteByte('\n')
		_ = out.Flush()
	}
	return sc.Err()
}
