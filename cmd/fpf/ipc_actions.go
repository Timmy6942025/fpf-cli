package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func maybeRunIPCReloadAction(args []string) (bool, int) {
	hasAction, query := parseIPCRequest(args, "--ipc-reload")
	if !hasAction {
		return false, 0
	}

	if err := runIPCReload(query); err != nil {
		return true, 1
	}

	return true, 0
}

func maybeRunIPCQueryNotifyAction(args []string) (bool, int) {
	hasAction, query := parseIPCRequest(args, "--ipc-query-notify")
	if !hasAction {
		return false, 0
	}

	minChars := parseEnvInt("FPF_RELOAD_MIN_CHARS", 2)
	if len(query) < minChars {
		if err := runIPCReload(query); err != nil {
			return true, 1
		}
		return true, 0
	}

	if err := runIPCReload(query); err != nil {
		return true, 1
	}

	return true, 0
}

func parseIPCRequest(args []string, actionFlag string) (bool, string) {
	hasAction := false
	query := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case actionFlag:
			hasAction = true
		case "--":
			if i+1 < len(args) {
				query = args[i+1]
			}
			return hasAction, query
		}
	}

	return hasAction, query
}

func runIPCReload(query string) error {
	fallbackFile := strings.TrimSpace(os.Getenv("FPF_IPC_FALLBACK_FILE"))
	if fallbackFile == "" {
		return fmt.Errorf("missing FPF_IPC_FALLBACK_FILE")
	}
	if _, err := os.Stat(fallbackFile); err != nil {
		return err
	}

	managerOverride := normalizeManagerName(strings.TrimSpace(os.Getenv("FPF_IPC_MANAGER_OVERRIDE")))
	if managerOverride != "" && !isManagerSupported(managerOverride) {
		return fmt.Errorf("unsupported manager override")
	}

	managerListCSV := strings.TrimSpace(os.Getenv("FPF_IPC_MANAGER_LIST"))
	bypassQueryCache := strings.TrimSpace(os.Getenv("FPF_BYPASS_QUERY_CACHE"))
	if bypassQueryCache == "" {
		bypassQueryCache = "0"
	}

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
	fzfPort := strings.TrimSpace(os.Getenv("FZF_PORT"))
	if fzfPort == "" {
		return fmt.Errorf("missing FZF_PORT")
	}

	host := "127.0.0.1"
	port := fzfPort
	if strings.Contains(fzfPort, ":") {
		host, port, _ = strings.Cut(fzfPort, ":")
		host = strings.TrimSpace(host)
		port = strings.TrimSpace(port)
		if host == "" {
			host = "127.0.0.1"
		}
	}

	targetURL := fmt.Sprintf("http://%s:%s", host, port)

	curlCmd := exec.Command(
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
	}

	httpRequest := "POST / HTTP/1.1\r\n" +
		fmt.Sprintf("Host: %s:%s\r\n", host, port) +
		"Content-Type: text/plain\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(actionPayload)) +
		"\r\n" +
		actionPayload

	ncCmd := exec.Command("nc", "-w", "2", host, port)
	ncCmd.Env = os.Environ()
	ncCmd.Stdin = strings.NewReader(httpRequest)
	ncCmd.Stdout = os.Stdout
	ncCmd.Stderr = os.Stderr

	return ncCmd.Run()
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
