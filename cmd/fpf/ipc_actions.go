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

	h, port := hostPort[0], hostPort[1]
	payload := fmt.Sprintf("reload %s\n", query)

	// Use timeout(1) wrapper for compatibility across Linux/macOS
	// timeout on Linux, gtimeout on macOS (coreutils)
	ncCmd := []string{h, port}
	timeoutCmd := "timeout"
	if _, err := exec.LookPath("timeout"); err != nil {
		timeoutCmd = "gtimeout"
	}

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
