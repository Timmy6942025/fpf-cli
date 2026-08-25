package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func maybeRunIPCReloadAction(args []string) (bool, int) {
	hasAction, query := parseIPCRequest(args, "--ipc-reload")
	if !hasAction {
		return false, 0
	}
	if err := runIPCReload(query); err != nil {
		fmt.Fprintf(os.Stderr, "fzf IPC reload failed: %v\n", err)
		return true, 1
	}
	return true, 0
}

func maybeRunIPCQueryNotifyAction(args []string) (bool, int) {
	hasAction, query := parseIPCRequest(args, "--ipc-query-notify")
	if !hasAction {
		return false, 0
	}

	// Short queries reload from the cached fallback file rather than re-searching
	// all managers; the dynamic-reload path enforces this by emitting the fallback
	// directly. Here we still notify fzf (which triggers the same fallback emit
	// inside --dynamic-reload), so a single call suffices either way.
	if err := runIPCReload(query); err != nil {
		fmt.Fprintf(os.Stderr, "fzf IPC query-notify failed: %v\n", err)
		return true, 1
	}

	return true, 0
}

func runIPCReload(query string) error {
	if len(query) > maxQueryLength {
		return fmt.Errorf("query too long: %d > %d", len(query), maxQueryLength)
	}
	if strings.Contains(query, "\x00") {
		return fmt.Errorf("query contains null byte")
	}
	fallbackFile := strings.TrimSpace(os.Getenv("FPF_IPC_FALLBACK_FILE"))
	if fallbackFile == "" {
		return fmt.Errorf("missing FPF_IPC_FALLBACK_FILE")
	}
	if !isSafeIPCFallbackPath(fallbackFile) {
		return fmt.Errorf("invalid FPF_IPC_FALLBACK_FILE %q: must be under FPF_SESSION_TMP_ROOT or TempDir, absolute, no ..", fallbackFile)
	}
	if info, err := os.Lstat(fallbackFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("FPF_IPC_FALLBACK_FILE %q is a symlink, refusing to use", fallbackFile)
	}
	if _, err := os.Stat(fallbackFile); err != nil {
		return fmt.Errorf("fallback file stat failed %q: %w", fallbackFile, err)
	}

	managerOverride := normalizeManagerName(strings.TrimSpace(os.Getenv("FPF_IPC_MANAGER_OVERRIDE")))
	if managerOverride != "" && !isManagerSupported(managerOverride) {
		return fmt.Errorf("unsupported manager override %q", managerOverride)
	}

	managerListCSV := strings.TrimSpace(os.Getenv("FPF_IPC_MANAGER_LIST"))
	// Validate manager list CSV
	if managerListCSV != "" {
		for _, m := range strings.Split(managerListCSV, ",") {
			m = normalizeManagerName(strings.TrimSpace(m))
			if m == "" {
				continue
			}
			if !isManagerSupported(m) {
				return fmt.Errorf("unsupported manager in list: %q", m)
			}
		}
		if len(managerListCSV) > 256 {
			return fmt.Errorf("manager list too long")
		}
	}
	// Share the same default as the change:reload transport so FPF_BYPASS_QUERY_CACHE
	// behaves identically regardless of which reload path is active.
	bypassQueryCache := dynamicReloadBypassValueGo()

	reloadCmd := buildDynamicReloadCommandForQuery(managerOverride, fallbackFile, managerListCSV, query, bypassQueryCache)
	actionPayload := "change-prompt(Search> )+reload(" + reloadCmd + ")"

	return sendFzfListenAction(actionPayload)
}

func buildDynamicReloadCommandForQuery(managerOverride, fallbackFile, managerListCSV, queryValue, bypassQueryCache string) string {
	parts := []string{
		"FPF_SKIP_INSTALLED_MARKERS=1",
		"FPF_BYPASS_QUERY_CACHE=" + shellQuote(bypassQueryCache),
		"FPF_SKIP_QUERY_CACHE_WRITE=1",
		"FPF_IPC_MANAGER_OVERRIDE=" + shellQuote(managerOverride),
		"FPF_IPC_MANAGER_LIST=" + shellQuote(managerListCSV),
		"FPF_IPC_FALLBACK_FILE=" + shellQuote(fallbackFile),
		shellQuote(os.Args[0]),
		"--dynamic-reload",
		"--",
		shellQuote(queryValue),
	}

	return strings.Join(parts, " ")
}

func sendFzfListenAction(actionPayload string) error {
	if len(actionPayload) > 8192 {
		return fmt.Errorf("action payload too large: %d", len(actionPayload))
	}
	fzfHost := strings.TrimSpace(os.Getenv("FPF_FZF_LISTEN_HOST"))
	if fzfHost == "" {
		fzfPort := strings.TrimSpace(os.Getenv("FZF_PORT"))
		if fzfPort != "" {
			fzfHost = fzfPort
		}
	}

	if fzfHost == "" {
		errMsg := "fpf: cannot determine fzf listen port: neither FPF_FZF_LISTEN_HOST nor FZF_PORT is set"
		fmt.Fprintln(os.Stderr, errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	// Single-pass host:port parsing.
	// Accepts: "9999", "localhost:9999", "127.0.0.1:9999"
	host := "127.0.0.1"
	port := strings.TrimSpace(fzfHost)
	if idx := strings.LastIndex(port, ":"); idx >= 0 {
		h, p := strings.TrimSpace(port[:idx]), strings.TrimSpace(port[idx+1:])
		if h != "" {
			host = h
		}
		port = p
	}
	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("invalid port in FPF listen address %q", fzfHost)
	}
	if host == "" || len(host) > 253 || strings.ContainsAny(host, " \t/\\") {
		return fmt.Errorf("invalid host in FPF listen address %q", fzfHost)
	}

	targetURL := fmt.Sprintf("http://%s:%s", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	curlCmd := exec.CommandContext(ctx,
		"curl",
		"--silent",
		"--show-error",
		"--fail",
		"--max-time",
		"2",
		"-H",
		"Content-Type: text/plain",
		"--data-binary",
		actionPayload,
		targetURL,
	)
	curlCmd.Env = os.Environ()
	curlCmd.Stdin = os.Stdin
	curlCmd.Stdout = os.Stdout
	curlCmd.Stderr = os.Stderr
	if err := curlCmd.Run(); err == nil {
		return nil
	} else if ctx.Err() == context.DeadlineExceeded {
		fmt.Fprintln(os.Stderr, "fpf warning: curl timed out")
	}

	// Fallback via nc with timeout
	httpRequest := "POST / HTTP/1.1\r\n" +
		fmt.Sprintf("Host: %s:%s\r\n", host, port) +
		"Content-Type: text/plain\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(actionPayload)) +
		"\r\n" +
		actionPayload

	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	ncCmd := exec.CommandContext(ctx2, "nc", "-w", "2", host, port)
	ncCmd.Env = os.Environ()
	ncCmd.Stdin = strings.NewReader(httpRequest)
	ncCmd.Stdout = os.Stdout
	ncCmd.Stderr = os.Stderr

	return ncCmd.Run()
}

// parseIPCRequest parses CLI args for an IPC action flag and extracts the query.
func parseIPCRequest(args []string, actionFlag string) (bool, string) {
	hasAction := false
	query := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case actionFlag:
			hasAction = true
		case "--":
			if i+1 < len(args) {
				query = strings.Join(args[i+1:], " ")
			}
			return hasAction, query
		}
	}

	return hasAction, query
}

// shellQuote wraps a string in single quotes, escaping any internal single quotes.
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
