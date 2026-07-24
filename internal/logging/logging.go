package logging

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/go-logr/logr"
	uzap "go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Options controls process-wide logging.
type Options struct {
	// Format is "console" (default, human-readable) or "json".
	Format string
	// Level is debug|info|error (default info).
	Level string
	// Development enables zap development mode (stacktraces on warn). Prefer false in cluster.
	Development bool
}

// FromEnv reads LOG_FORMAT / LOG_LEVEL / LOG_DEV.
func FromEnv() Options {
	return Options{
		Format:      strings.ToLower(envOr("LOG_FORMAT", "console")),
		Level:       strings.ToLower(envOr("LOG_LEVEL", "info")),
		Development: envBool("LOG_DEV", false),
	}
}

// Setup configures the controller-runtime global logger and returns a named logger.
func Setup(component string, o Options) logr.Logger {
	if o.Format == "" {
		o.Format = "console"
	}
	if o.Level == "" {
		o.Level = "info"
	}

	encCfg := uzap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.LevelKey = "level"
	encCfg.NameKey = "logger"
	encCfg.CallerKey = "" // quieter console; enable via LOG_CALLER=1
	if envBool("LOG_CALLER", false) {
		encCfg.CallerKey = "caller"
		encCfg.EncodeCaller = zapcore.ShortCallerEncoder
	}
	encCfg.MessageKey = "msg"
	encCfg.StacktraceKey = "stacktrace"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeDuration = zapcore.StringDurationEncoder
	if o.Format == "console" {
		encCfg.EncodeLevel = zapcore.CapitalLevelEncoder
		encCfg.ConsoleSeparator = " "
	} else {
		encCfg.EncodeLevel = zapcore.LowercaseLevelEncoder
	}

	var encoder zapcore.Encoder
	if o.Format == "json" {
		encoder = zapcore.NewJSONEncoder(encCfg)
	} else {
		encoder = zapcore.NewConsoleEncoder(encCfg)
	}

	lvl := zapcore.InfoLevel
	switch o.Level {
	case "debug", "dbg":
		lvl = zapcore.DebugLevel
	case "warn", "warning":
		lvl = zapcore.WarnLevel
	case "error", "err":
		lvl = zapcore.ErrorLevel
	}

	opts := crzap.Options{
		Development: o.Development,
		Level:       lvl,
		Encoder:     encoder,
		ZapOpts: []uzap.Option{
			uzap.AddCallerSkip(0),
		},
	}
	_ = flag.CommandLine
	ctrl.SetLogger(crzap.New(crzap.UseFlagOptions(&opts)))
	return ctrl.Log.WithName(component)
}

// Truncate shortens s for safe log fields (never dumps huge JSON/JWT payloads at info).
func Truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 {
		max = 200
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("…(%dB)", len(s))
}

// RedactURL strips JWT/token path segments from download URLs for info logs.
func RedactURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Truncate(raw, 80)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	for i, p := range parts {
		if strings.Count(p, ".") == 2 && len(p) > 40 {
			parts[i] = "<token>"
		}
	}
	path := "/" + strings.Join(parts, "/")
	if strings.HasSuffix(raw, "/") && !strings.HasSuffix(path, "/") {
		path += "/"
	}
	// Rebuild without url.URL.String() so "<token>" is not percent-encoded.
	out := u.Scheme + "://" + u.Host + path
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

// BodySummary is a short debug-friendly description of a request/response payload.
// Full bodies stay out of info logs; at debug we only log size + truncated preview.
func BodySummary(data []byte, maxPreview int) string {
	if len(data) == 0 {
		return "empty"
	}
	if maxPreview <= 0 {
		maxPreview = 240
	}
	preview := Truncate(string(data), maxPreview)
	// Prefer compact JSON key list when possible.
	var obj map[string]json.RawMessage
	if json.Unmarshal(data, &obj) == nil && len(obj) > 0 {
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		if len(keys) > 12 {
			keys = append(keys[:12], "…")
		}
		return fmt.Sprintf("%dB keys=%v", len(data), keys)
	}
	return fmt.Sprintf("%dB preview=%q", len(data), preview)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return def
	}
}
