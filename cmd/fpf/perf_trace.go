package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	perfTraceWriter io.Writer = os.Stderr
	perfTraceMu     sync.Mutex
)

func perfTraceEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_PERF_TRACE")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func logPerfTraceStage(stage string, started time.Time) {
	logPerfTraceStageDetail(stage, "", started)
}

func logPerfTraceStageDetail(stage, detail string, started time.Time) {
	if !perfTraceEnabled() {
		return
	}
	duration := time.Since(started).Milliseconds()

	perfTraceMu.Lock()
	defer perfTraceMu.Unlock()

	if detail == "" {
		_, _ = fmt.Fprintf(perfTraceWriter, "perf-trace stage=%s duration_ms=%d\n", stage, duration)
		return
	}
	_, _ = fmt.Fprintf(perfTraceWriter, "perf-trace stage=%s detail=%s duration_ms=%d\n", stage, detail, duration)
}
