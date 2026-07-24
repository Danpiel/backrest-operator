package main

import (
	"crypto/tls"
	"flag"
	"net/http"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	operatorv1alpha1 "github.com/Danpiel/backrest-operator/api/v1alpha1"
	"github.com/Danpiel/backrest-operator/internal/controller"
	"github.com/Danpiel/backrest-operator/internal/filters"
	"github.com/Danpiel/backrest-operator/internal/logging"
	"github.com/Danpiel/backrest-operator/internal/webhook"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(netv1.AddToScheme(scheme))
}

func main() {
	var metricsAddr string
	var probeAddr string
	var enableWebhooks bool
	var webhookAddr string
	var webhookCertDir string
	flag.StringVar(&metricsAddr, "metrics-bind-address", envAddr("METRICS_PORT", ":8080"), "metrics endpoint")
	flag.StringVar(&probeAddr, "health-probe-bind-address", envAddr("HEALTH_PORT", ":8081"), "health probe endpoint")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", envBool("WEBHOOK_ENABLED", true), "enable validating webhooks")
	flag.StringVar(&webhookAddr, "webhook-bind-address", envAddr("WEBHOOK_PORT", ":9443"), "webhook listen address")
	flag.StringVar(&webhookCertDir, "webhook-cert-dir", envOr("WEBHOOK_CERT_DIR", "/tls"), "TLS cert directory (tls.crt/tls.key)")
	flag.Parse()

	setupLog := logging.Setup("operator", logging.FromEnv())
	setupLog.Info("starting backrest-operator",
		"logFormat", envOr("LOG_FORMAT", "console"),
		"logLevel", envOr("LOG_LEVEL", "info"),
		"metrics", metricsAddr,
		"health", probeAddr,
		"webhooks", enableWebhooks,
	)

	mgrOpts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         false,
	}
	if ns := filters.CacheNamespaces(); len(ns) > 0 {
		namespaces := map[string]cache.Config{}
		for _, n := range ns {
			namespaces[n] = cache.Config{}
		}
		mgrOpts.Cache = cache.Options{DefaultNamespaces: namespaces}
		setupLog.Info("watch filter enabled", "namespaces", ns)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), mgrOpts)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err := (&controller.BackrestClusterReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "register controller", "name", "BackrestCluster")
		os.Exit(1)
	}
	if err := (&controller.BackupRepositoryReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "register controller", "name", "BackupRepository")
		os.Exit(1)
	}
	if err := (&controller.BackupPlanReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "register controller", "name", "BackupPlan")
		os.Exit(1)
	}
	if err := (&controller.PVCBackupReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "register controller", "name", "PVCBackup")
		os.Exit(1)
	}
	if err := (&controller.PVCRestoreReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "register controller", "name", "PVCRestore")
		os.Exit(1)
	}
	setupLog.Info("controllers registered")

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "readyz")
		os.Exit(1)
	}

	if enableWebhooks {
		go func() {
			srv := &http.Server{Addr: webhookAddr, Handler: webhook.Handler()}
			cert := webhookCertDir + "/tls.crt"
			key := webhookCertDir + "/tls.key"
			if _, err := os.Stat(cert); err == nil {
				srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
				setupLog.Info("webhook listening", "addr", webhookAddr, "tls", true)
				if err := srv.ListenAndServeTLS(cert, key); err != nil {
					setupLog.Error(err, "webhook server stopped")
				}
			} else {
				setupLog.Info("webhook listening", "addr", webhookAddr, "tls", false)
				if err := srv.ListenAndServe(); err != nil {
					setupLog.Error(err, "webhook server stopped")
				}
			}
		}()
	}

	setupLog.Info("manager running")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "manager stopped")
		os.Exit(1)
	}
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "TRUE", "yes", "YES":
		return true
	case "0", "false", "FALSE", "no", "NO":
		return false
	default:
		return def
	}
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
	if strings.HasPrefix(v, ":") {
		return v
	}
	return ":" + v
}
