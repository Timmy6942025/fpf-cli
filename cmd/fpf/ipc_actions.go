package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
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

func runIPCReload(query string) error {
	fzfHost := os.Getenv("FPF_FZF_LISTEN_HOST")
	if fzfHost == "" {
		fzfHost = "localhost:9812"
	}

	// curl with retries (primary)
	body := buildIPCBody(query)
	maxRetries := 3
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(50 * time.Millisecond)
		}
		cmd := exec.Command("curl", "-s", "-X", "POST",
			"--max-time", "3",
			"-d", body,
			fmt.Sprintf("http://%s/reload", fzfHost))
		_, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		if attempt == maxRetries-1 {
			break
		}
	}

	// nc fallback with retry loop (3 retries per binary, then next binary)
	ncBinaries := []string{"nc", "netcat", "nc.openbsd"}
	for _, ncBinary := range ncBinaries {
		if !commandExists(ncBinary) {
			continue
		}
		for attempt := 0; attempt < maxRetries; attempt++ {
			if attempt > 0 {
				time.Sleep(50 * time.Millisecond)
			}
			err := sendViaNetcat(fzfHost, query)
			if err == nil {
				return nil
			}
		}
	}

	return fmt.Errorf("all IPC mechanisms failed")
}

func sendViaNetcat(addr string, query string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}

	body := buildIPCBody(query)
	req := fmt.Sprintf("POST /reload HTTP/1.1\r\nHost: %s\r\nContent-Type: text/plain\r\nContent-Length: %d\r\n\r\n%s",
		addr, len(body), body)

	binary, args := resolveNetcatVariant(host, port)
	cmd := exec.Command(binary, args...)
	cmd.Stdin = strings.NewReader(req)
	cmd.Stdout = os.Stdin
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func resolveNetcatVariant(host, port string) (string, []string) {
	cmd := exec.Command("nc", "-h")
	out, _ := cmd.Output()
	if strings.Contains(string(out), "-q") {
		return "nc", []string{"-q", "0", host, port}
	}
	if strings.Contains(string(out), "-c") {
		return "nc", []string{"-c", "echo ''", host, port}
	}
	return "timeout", []string{"2", "nc", host, port}
}

func buildIPCBody(query string) string {
	if query == "" {
		return ""
	}
	return fmt.Sprintf("\x1b]49;%s\x07", query)
}

func commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// parseIPCRequest is retained from original for compatibility
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

// shellQuote is retained as it's used by cli_runtime.go
func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
