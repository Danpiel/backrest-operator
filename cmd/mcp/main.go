package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/Reactive-Network/backrest-operator/internal/logging"
	"github.com/Reactive-Network/backrest-operator/internal/mcp"
)

var authDenials = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "backrest_mcp_auth_denials_total",
	Help: "MCP authorization denials",
}, []string{"tool"})

func init() {
	prometheus.MustRegister(authDenials)
}

func main() {
	var (
		mode        string
		listenAddr  string
		metricsAddr string
		requireAuth bool
	)
	flag.StringVar(&mode, "mode", envOr("MCP_MODE", "http"), "http or stdio")
	flag.StringVar(&listenAddr, "listen", envAddr("MCP_PORT", ":8081"), "HTTP listen address")
	flag.StringVar(&metricsAddr, "metrics", envAddr("METRICS_PORT", ":8080"), "metrics listen address")
	flag.BoolVar(&requireAuth, "require-auth", envBoolDefault("MCP_REQUIRE_AUTH", true), "require Kubernetes bearer TokenReview on HTTP /mcp (disable behind private Ingress)")
	flag.Parse()

	log := logging.Setup("mcp", logging.FromEnv())
	log.Info("starting backrest-mcp",
		"mode", mode,
		"listen", listenAddr,
		"metrics", metricsAddr,
		"requireAuth", requireAuth,
		"logFormat", envOr("LOG_FORMAT", "console"),
		"logLevel", envOr("LOG_LEVEL", "info"),
	)

	cfg, err := restConfig()
	if err != nil {
		log.Error(err, "load kubeconfig")
		os.Exit(1)
	}

	srv, err := mcp.NewServer(cfg, func(tool string) {
		authDenials.WithLabelValues(tool).Inc()
		log.Info("auth denied", "tool", tool)
	}, requireAuth)
	if err != nil {
		log.Error(err, "create MCP server")
		os.Exit(1)
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Info("metrics listening", "addr", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Error(err, "metrics server stopped")
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if mode == "stdio" {
		log.Info("serving MCP over stdio")
		if err := srv.RunStdio(ctx); err != nil {
			log.Error(err, "stdio stopped")
			os.Exit(1)
		}
		return
	}

	httpSrv := &http.Server{Addr: listenAddr, Handler: srv.HTTPHandler()}
	go func() {
		<-ctx.Done()
		log.Info("shutting down HTTP server")
		_ = httpSrv.Shutdown(context.Background())
	}()
	log.Info("serving MCP over HTTP", "addr", listenAddr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error(err, "HTTP server stopped")
		os.Exit(1)
	}
}

func restConfig() (*rest.Config, error) {
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	kubeCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	return kubeCfg.ClientConfig()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envBoolDefault(key string, def bool) bool {
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

func envAddr(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if len(v) > 0 && v[0] == ':' {
		return v
	}
	return fmt.Sprintf(":%s", v)
}
