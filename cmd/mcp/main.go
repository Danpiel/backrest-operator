package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/Danpiel/backrest-operator/internal/mcp"
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
	)
	flag.StringVar(&mode, "mode", envOr("MCP_MODE", "http"), "http or stdio")
	flag.StringVar(&listenAddr, "listen", envAddr("MCP_PORT", ":8081"), "HTTP listen address")
	flag.StringVar(&metricsAddr, "metrics", envAddr("METRICS_PORT", ":8080"), "metrics listen address")
	opts := zap.Options{Development: true}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("mcp")

	cfg, err := restConfig()
	if err != nil {
		log.Error(err, "kubeconfig")
		os.Exit(1)
	}

	srv, err := mcp.NewServer(cfg, func(tool string) {
		authDenials.WithLabelValues(tool).Inc()
	})
	if err != nil {
		log.Error(err, "create mcp server")
		os.Exit(1)
	}

	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		_ = http.ListenAndServe(metricsAddr, mux)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if mode == "stdio" {
		log.Info("starting MCP stdio")
		if err := srv.RunStdio(ctx); err != nil {
			log.Error(err, "stdio")
			os.Exit(1)
		}
		return
	}

	httpSrv := &http.Server{Addr: listenAddr, Handler: srv.HTTPHandler()}
	go func() {
		<-ctx.Done()
		_ = httpSrv.Shutdown(context.Background())
	}()
	log.Info("starting MCP HTTP", "addr", listenAddr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error(err, "http server")
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
