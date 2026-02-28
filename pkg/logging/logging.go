package logging

import (
	"log/slog"
	"os"
)

// Setup configures the default slog logger. On Cloud Run (K_SERVICE is set),
// it uses a JSON handler with field mappings compatible with Google Cloud
// Logging structured logging. Otherwise it uses a text handler for local
// development.
func Setup() {
	if os.Getenv("K_SERVICE") != "" {
		setupCloudLogging()
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	}
}

func setupCloudLogging() {
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.MessageKey:
				a.Key = "message"
			case slog.LevelKey:
				a.Key = "severity"
				if a.Value.String() == "WARN" {
					a.Value = slog.StringValue("WARNING")
				}
			case slog.TimeKey:
				a.Key = "timestamp"
			}
			return a
		},
	})
	slog.SetDefault(slog.New(h))
}
