package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// Alert-facing metrics (names must match VMRule / PrometheusRule).
	BackupFailedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backrest_backup_failed_total",
		Help: "PVC backup failures (alert: BackrestBackupFailed)",
	}, []string{"namespace", "name"})

	BackupLastSuccessSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "backrest_backup_last_success_timestamp_seconds",
		Help: "Unix timestamp of last successful backup (alert SLA)",
	}, []string{"namespace", "name"})

	RestoreFailedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backrest_restore_failed_total",
		Help: "PVC restore failures (alert: BackrestRestoreFailed)",
	}, []string{"namespace", "name"})

	// Legacy / detailed counters kept for dashboards.
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
		Help: "Unix timestamp of last successful backup (legacy name)",
	}, []string{"namespace", "name"})

	ReconcileErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "backrest_operator_reconcile_errors_total",
		Help: "Reconcile errors",
	}, []string{"kind"})
)

func init() {
	prometheus.MustRegister(
		BackupFailedTotal,
		BackupLastSuccessSeconds,
		RestoreFailedTotal,
		BackupTotal,
		BackupDuration,
		BackupLastSuccess,
		ReconcileErrors,
	)
}

// ObserveBackupSuccess updates success metrics used by SLA alerts.
func ObserveBackupSuccess(namespace, name string, unixTs float64) {
	BackupTotal.WithLabelValues(namespace, name, "success").Inc()
	BackupLastSuccess.WithLabelValues(namespace, name).Set(unixTs)
	BackupLastSuccessSeconds.WithLabelValues(namespace, name).Set(unixTs)
}

// ObserveBackupFailure increments failure counters used by BackrestBackupFailed.
func ObserveBackupFailure(namespace, name string) {
	BackupTotal.WithLabelValues(namespace, name, "failure").Inc()
	BackupFailedTotal.WithLabelValues(namespace, name).Inc()
}

// ObserveRestoreFailure increments restore failure counters.
func ObserveRestoreFailure(namespace, name string) {
	RestoreFailedTotal.WithLabelValues(namespace, name).Inc()
}

// StartServer serves /metrics on addr (e.g. :8080).
func StartServer(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		_ = http.ListenAndServe(addr, mux)
	}()
}
