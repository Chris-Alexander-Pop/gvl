package trace

import (
	"log"
	"os"
	"strings"
	"sync/atomic"
)

var on atomic.Bool

// InitFromEnv enables tracing when GVL_DEBUG is 1/true/yes.
func InitFromEnv() {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("GVL_DEBUG")))
	if v == "1" || v == "true" || v == "yes" {
		Enable()
	}
}

// Enable turns on stderr timing logs.
func Enable() { on.Store(true) }

// Enabled reports whether tracing is on.
func Enabled() bool { return on.Load() }

// Printf logs to stderr when tracing is enabled.
func Printf(format string, args ...any) {
	if on.Load() {
		log.Printf(format, args...)
	}
}
