package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	BackupTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backrest_operator_backup_total",
		Help: "PVC backup attempts",
	}, []string{"namespace", "name", "result"})

	BackupDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "backrest_operator_backup_duration_seconds",
		Help:    "PVC backup duration",
		Buckets: []float64{30, 60, 120, 300, 600, 1800, 3600, 7200},
	}, []string{"namespace", "name"})

	BackupLastSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "backrest_operator_backup_last_success_timestamp",
		Help: "Unix timestamp of last successful backup",
	}, []string{"namespace", "name"})

	ReconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backrest_operator_reconcile_errors_total",
		Help: "Reconcile errors",
	}, []string{"kind"})
)

func init() {
	prometheus.MustRegister(BackupTotal, BackupDuration, BackupLastSuccess, ReconcileErrors)
}

// StartServer serves /metrics on addr (e.g. :8080).
func StartServer(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		_ = http.ListenAndServe(addr, mux)
	}()
}
