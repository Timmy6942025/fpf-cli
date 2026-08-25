package main

import (
	"fmt"
	"os"
	"strings"
)

// log levels: debug, info, warn, error
// Controlled via FPF_DEBUG, FPF_DEBUG_RELOAD, FPF_DEBUG_SELECTION, FPF_PERF_TRACE

func logDebugf(format string, args ...interface{}) {
	if !debugEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "debug: "+format+"\n", args...)
}

func logDebugReloadf(format string, args ...interface{}) {
	if !debugReloadEnabled() {
		return
	}
	fmt.Fprintf(os.Stderr, "fpf debug(reload): "+format+"\n", args...)
}

func logDebugSelectionf(format string, args ...interface{}) {
	if !selectionDebugEnabledGo() {
		return
	}
	fmt.Fprintf(os.Stderr, "debug(selection): "+format+"\n", args...)
}

func logErrorf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fpf: "+format+"\n", args...)
}

func logWarnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "fpf: warning: "+format+"\n", args...)
}

func debugEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DEBUG")))
	return v == "1" || v == "true" || v == "yes" || v == "on" || v == "debug"
}
