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

	// Try curl first (fzf >= 0.42.0 with change:reload:)
	if err := runCurlReload(fzfHost, query); err == nil {
		return nil
	}

	// Fallback to nc (netcat) for older fzf versions
	return runNetcatReload(fzfHost, query)
}

func runCurlReload(host, query string) error {
	// fzf 0.42+ supports change:reload: which sends HTTP/1.1 POST
	url := "http://" + host + "/fzf_reload"
	payload := "reload " + query

	cmd := exec.Command("curl", "-s", "-X", "POST", "-d", payload,
		"--max-time", "1",
		"-H", "Content-Type: application/octet-stream",
		url)

	// Run without waiting — fire and forget, fzf handles it
	cmd.Start()
	cmd.Process.Release()
	return nil
}

func runNetcatReload(host, query string) error {
	// For older fzf: use netcat to send fzf IPC protocol message
	// Protocol: one line per event, \n terminated
	// Event format: "event payload\n"
	hostPort := strings.Split(host, ":")
	if len(hostPort) != 2 {
		return fmt.Errorf("invalid FPF_FZF_LISTEN_HOST: %s (expected host:port)", host)
	}

	payload := fmt.Sprintf("reload %s\n", query)

	// Use timeout(1) wrapper for compatibility across Linux/macOS
	// timeout on Linux, gtimeout on macOS (coreutils)
	timeoutCmd := "timeout"
	if _, err := exec.LookPath("timeout"); err != nil {
		timeoutCmd = "gtimeout"
	}
	_ = timeoutCmd // reserved for future netcat variant use

	conn, err := net.DialTimeout("tcp", host, 1*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to fzf at %s: %w", host, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(1 * time.Second))

	_, err = conn.Write([]byte(payload))
	if err != nil {
		return fmt.Errorf("sending reload to fzf: %w", err)
	}

	// Read response (fzf sends back the reload result)
	buf := make([]byte, 1024)
	conn.Read(buf)

	return nil
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
				query = args[i+1]
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
