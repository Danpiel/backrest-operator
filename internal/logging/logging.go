package logging

import (
	"flag"
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
	encCfg.CallerKey = "caller"
	encCfg.MessageKey = "msg"
	encCfg.StacktraceKey = "stacktrace"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
	encCfg.EncodeDuration = zapcore.StringDurationEncoder
	encCfg.EncodeCaller = zapcore.ShortCallerEncoder
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
	// Allow CLI overrides when flags are registered by the caller.
	_ = flag.CommandLine
	ctrl.SetLogger(crzap.New(crzap.UseFlagOptions(&opts)))
	return ctrl.Log.WithName(component)
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
