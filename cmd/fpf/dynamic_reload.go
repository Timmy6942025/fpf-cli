package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

func maybeRunDynamicReloadAction(args []string) (bool, int) {
	hasDynamicReload, query := parseDynamicReloadRequest(args)
	if !hasDynamicReload {
		return false, 0
	}

	fallbackFile := strings.TrimSpace(os.Getenv("FPF_IPC_FALLBACK_FILE"))
	if fallbackFile == "" {
		return true, 1
	}
	if _, err := os.Stat(fallbackFile); err != nil {
		return true, 1
	}

	minChars := parseEnvInt("FPF_RELOAD_MIN_CHARS", 2)
	if len(query) < minChars {
		emitFile(fallbackFile)
		return true, 0
	}

	reloadDebounce := parseEnvFloat("FPF_RELOAD_DEBOUNCE", 0.12)
	if reloadDebounce > 0 {
		time.Sleep(time.Duration(reloadDebounce * float64(time.Second)))
	}

	managerArg, ok := resolveReloadManagerArg()
	if !ok {
		emitFile(fallbackFile)
		return true, 0
	}

	out, err := runFeedSearchQuery(query, managerArg)
	if err != nil {
		emitFile(fallbackFile)
		return true, 0
	}

	_, _ = os.Stdout.Write(out)
	return true, 0
}

func parseDynamicReloadRequest(args []string) (bool, string) {
	hasDynamicReload := false
	query := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dynamic-reload":
			hasDynamicReload = true
		case "--":
			if i+1 < len(args) {
				query = args[i+1]
			}
			return hasDynamicReload, query
		}
	}

	return hasDynamicReload, query
}

func resolveReloadManagerArg() (string, bool) {
	managerOverride := normalizeManagerName(strings.TrimSpace(os.Getenv("FPF_IPC_MANAGER_OVERRIDE")))
	managerListCSV := strings.TrimSpace(os.Getenv("FPF_IPC_MANAGER_LIST"))

	if managerOverride != "" {
		if !isManagerSupported(managerOverride) || !isManagerCommandReady(managerOverride) {
			return "", false
		}
		return managerOverride, true
	}

	if managerListCSV == "" {
		return "", true
	}

	parts := strings.Split(managerListCSV, ",")
	seen := map[string]struct{}{}
	managers := make([]string, 0, len(parts))
	for _, part := range parts {
		manager := normalizeManagerName(strings.TrimSpace(part))
		if manager == "" {
			continue
		}
		if !isManagerSupported(manager) || !isManagerCommandReady(manager) {
			continue
		}
		if _, exists := seen[manager]; exists {
			continue
		}
		seen[manager] = struct{}{}
		managers = append(managers, manager)
	}

	if len(managers) == 0 {
		return "", false
	}

	return strings.Join(managers, ","), true
}

func runFeedSearchQuery(query string, managerArg string) ([]byte, error) {
	managers := splitManagerArg(managerArg)
	if len(managers) <= 1 {
		return runSingleFeedSearchQuery(query, managerArg)
	}

	type managerResult struct {
		index int
		out   []byte
		err   error
	}

	results := make([][]byte, len(managers))
	ch := make(chan managerResult, len(managers))
	var wg sync.WaitGroup

	for idx, manager := range managers {
		wg.Add(1)
		go func(index int, managerName string) {
			defer wg.Done()
			out, err := runSingleFeedSearchQuery(query, managerName)
			ch <- managerResult{index: index, out: out, err: err}
		}(idx, manager)
	}

	wg.Wait()
	close(ch)

	var firstErr error
	for result := range ch {
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
			}
			continue
		}
		if len(bytes.TrimSpace(result.out)) == 0 {
			continue
		}
		results[result.index] = result.out
	}

	outputs := make([][]byte, 0, len(managers))
	for _, out := range results {
		if len(out) == 0 {
			continue
		}
		outputs = append(outputs, out)
	}

	if len(outputs) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return []byte{}, nil
	}

	return mergeFeedOutputs(outputs), nil
}

func runSingleFeedSearchQuery(query string, managerArg string) ([]byte, error) {
	args := make([]string, 0, 6)
	if managerArg != "" {
		args = append(args, "--manager", managerArg)
	}
	args = append(args, "--feed-search", "--", query)

	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "FPF_SKIP_INSTALLED_MARKERS=1")
	cmd.Stderr = io.Discard

	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	return out, nil
}

func splitManagerArg(managerArg string) []string {
	trimmed := strings.TrimSpace(managerArg)
	if trimmed == "" {
		return nil
	}

	parts := strings.Split(trimmed, ",")
	managers := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		manager := strings.TrimSpace(part)
		if manager == "" {
			continue
		}
		if _, ok := seen[manager]; ok {
			continue
		}
		seen[manager] = struct{}{}
		managers = append(managers, manager)
	}

	return managers
}

func mergeFeedOutputs(outputs [][]byte) []byte {
	var merged bytes.Buffer
	seen := map[string]struct{}{}

	for _, output := range outputs {
		for _, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			merged.WriteString(line)
			merged.WriteString("\n")
		}
	}

	return merged.Bytes()
}

func emitFile(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(raw)
}

func parseEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return value
}

func parseEnvFloat(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return value
}

func isManagerSupported(manager string) bool {
	switch manager {
	case "apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap", "flatpak", "npm", "bun":
		return true
	default:
		return false
	}
}

func isManagerCommandReady(manager string) bool {
	var binaries []string
	match := true

	switch manager {
	case "apt":
		binaries = []string{"apt-cache", "apt-get", "dpkg-query"}
	case "dnf":
		binaries = []string{"dnf"}
	case "pacman":
		binaries = []string{"pacman"}
	case "zypper":
		binaries = []string{"zypper"}
	case "emerge":
		binaries = []string{"emerge"}
	case "brew":
		binaries = []string{"brew"}
	case "winget":
		binaries = []string{"winget"}
	case "choco":
		binaries = []string{"choco"}
	case "scoop":
		binaries = []string{"scoop"}
	case "snap":
		binaries = []string{"snap"}
	case "flatpak":
		binaries = []string{"flatpak"}
	case "npm":
		binaries = []string{"npm"}
	case "bun":
		binaries = []string{"bun"}
	default:
		match = false
	}

	if !match {
		return false
	}

	for _, binary := range binaries {
		if _, err := exec.LookPath(binary); err != nil {
			return false
		}
	}

	return true
}
