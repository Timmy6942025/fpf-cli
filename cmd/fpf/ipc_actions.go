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
		// Fall back to $FZF_PORT environment variable set by fzf's --listen flag
		fzfPort := os.Getenv("FZF_PORT")
		if fzfPort != "" {
			// Accept either a bare port or a precomposed host:port
			if strings.Contains(fzfPort, ":") {
				fzfHost = fzfPort
			} else {
				fzfHost = "localhost:" + fzfPort
			}
		}
	}

	if fzfHost == "" {
		errMsg := "fpf: cannot determine fzf listen port: neither FPF_FZF_LISTEN_HOST nor FZF_PORT is set"
		fmt.Fprintln(os.Stderr, errMsg)
		return fmt.Errorf(errMsg)
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
	err := cmd.Start()
	if err != nil {
		return err
	}
	if cmd.Process != nil {
		cmd.Process.Release()
	}
	return nil
}

func runNetcatReload(host, query string) error {
	// For older fzf: send HTTP/1.1 POST request
	hostPort := strings.Split(host, ":")
	if len(hostPort) != 2 {
		return fmt.Errorf("invalid FPF_FZF_LISTEN_HOST: %s (expected host:port)", host)
	}

	body := fmt.Sprintf("reload(%s)", query)
	request := fmt.Sprintf("POST / HTTP/1.1\r\nHost: %s\r\nContent-Length: %d\r\n\r\n%s", hostPort[0], len(body), body)

	conn, err := net.DialTimeout("tcp", host, 1*time.Second)
	if err != nil {
		return fmt.Errorf("connecting to fzf at %s: %w", host, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(1 * time.Second))

	_, err = conn.Write([]byte(request))
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