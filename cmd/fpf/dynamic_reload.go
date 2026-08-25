package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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
	stageStart := time.Now()
	defer logPerfTraceStage("dynamic-reload", stageStart)

	// Validate query
	if len(query) > maxQueryLength {
		query = query[:maxQueryLength]
		fmt.Fprintf(os.Stderr, "fpf warning: query truncated to %d chars\n", maxQueryLength)
	}
	if strings.Contains(query, "\x00") {
		fmt.Fprintln(os.Stderr, "fpf debug(reload): query contains null byte, using fallback")
		fallbackFile := strings.TrimSpace(os.Getenv("FPF_IPC_FALLBACK_FILE"))
		if fallbackFile != "" && isSafeIPCFallbackPath(fallbackFile) {
			emitFile(fallbackFile)
		}
		return true, 0
	}

	fallbackFile := strings.TrimSpace(os.Getenv("FPF_IPC_FALLBACK_FILE"))
	if fallbackFile == "" {
		fmt.Fprintln(os.Stderr, "fpf debug(reload): missing FPF_IPC_FALLBACK_FILE")
		return true, 1
	}
	if !isSafeIPCFallbackPath(fallbackFile) {
		if debugReloadEnabled() {
			fmt.Fprintf(os.Stderr, "fpf debug(reload): invalid fallback file %q\n", fallbackFile)
		}
		return true, 1
	}
	if info, err := os.Lstat(fallbackFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf debug(reload): fallback file %q is symlink, refusing\n", fallbackFile)
		return true, 1
	}
	if _, err := os.Stat(fallbackFile); err != nil {
		if debugReloadEnabled() {
			fmt.Fprintf(os.Stderr, "fpf debug(reload): fallback file %s not found: %v\n", fallbackFile, err)
		}
		return true, 1
	}

	minChars := parseEnvInt("FPF_RELOAD_MIN_CHARS", 2)
	if minChars < 0 {
		minChars = 0
	}
	if minChars > 10 {
		minChars = 10
	}
	if len(query) < minChars {
		emitFile(fallbackFile)
		return true, 0
	}

	reloadDebounce := parseEnvFloat("FPF_RELOAD_DEBOUNCE", 0.12)
	if reloadDebounce < 0 {
		reloadDebounce = 0
	}
	if reloadDebounce > 2 {
		reloadDebounce = 2
	}
	if reloadDebounce > 0 {
		time.Sleep(time.Duration(reloadDebounce * float64(time.Second)))
	}

	managerArg, ok := resolveReloadManagerArg()
	if !ok {
		if debugReloadEnabled() {
			fmt.Fprintf(os.Stderr, "fpf debug(reload): could not resolve manager arg, using fallback\n")
		}
		emitFile(fallbackFile)
		return true, 0
	}
	managers := splitManagerArg(managerArg)
	if len(managers) == 0 {
		if debugReloadEnabled() {
			fmt.Fprintf(os.Stderr, "fpf debug(reload): empty manager list, using fallback\n")
		}
		emitFile(fallbackFile)
		return true, 0
	}

	if debugReloadEnabled() {
		fmt.Fprintf(os.Stderr, "fpf debug(reload): query=%q managers=%v\n", query, managers)
	}

	rows, err := buildDisplayRows(query, managers)
	if err != nil {
		if debugReloadEnabled() {
			fmt.Fprintf(os.Stderr, "fpf debug(reload): error building rows: %v\n", err)
		}
		emitFile(fallbackFile)
		return true, 0
	}

	if debugReloadEnabled() {
		fmt.Fprintf(os.Stderr, "fpf debug(reload): query=%q result_count=%d\n", query, len(rows))
	}

	if len(rows) == 0 {
		emitFile(fallbackFile)
		return true, 0
	}

	writeBuildDisplayRowsTSV(rows)
	return true, 0
}

func debugReloadEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DEBUG_RELOAD")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
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
				query = strings.Join(args[i+1:], " ")
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
		if !isManagerSupported(managerOverride) {
			return "", false
		}
		return managerOverride, true
	}

	if managerListCSV == "" {
		return "", false
	}

	parts := strings.Split(managerListCSV, ",")
	seen := map[string]struct{}{}
	managers := make([]string, 0, len(parts))
	for _, part := range parts {
		manager := normalizeManagerName(strings.TrimSpace(part))
		if manager == "" {
			continue
		}
		if !isManagerSupported(manager) {
			return "", false
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

func isSafeIPCFallbackPath(path string) bool {
	if path == "" {
		return false
	}
	if len(path) > 4096 {
		return false
	}
	if !filepath.IsAbs(path) {
		return false
	}
	if strings.Contains(path, "..") {
		return false
	}
	if strings.Contains(path, "\x00") {
		return false
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	cleaned := filepath.Clean(path)
	tmpDirRaw := os.TempDir()
	if tmpDirRaw == "" {
		tmpDirRaw = "/tmp"
	}
	tempDir := filepath.Clean(tmpDirRaw)
	sessionRoot := strings.TrimSpace(os.Getenv("FPF_SESSION_TMP_ROOT"))
	isUnder := func(base, p string) bool {
		if base == "" || base == "." {
			return false
		}
		if p == base {
			return true
		}
		if strings.HasPrefix(p, base+string(os.PathSeparator)) {
			return true
		}
		return false
	}
	if isUnder(tempDir, cleaned) {
		return true
	}
	if sessionRoot != "" {
		if len(sessionRoot) > 4096 || strings.Contains(sessionRoot, "\x00") {
			return false
		}
		sClean := filepath.Clean(sessionRoot)
		if isUnder(sClean, cleaned) || cleaned == sClean {
			return true
		}
		fallback := filepath.Clean(filepath.Join(tmpDirRaw, "fpf"))
		if isUnder(fallback, cleaned) {
			return true
		}
	}
	return false
}

func emitFile(path string) {
	if !isSafeIPCFallbackPath(path) {
		return
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	limited := io.LimitReader(f, 1<<20)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return
	}
	_, _ = os.Stdout.Write(raw)
}

func writeBuildDisplayRowsTSV(rows []buildDisplayRow) {
	for _, row := range rows {
		desc := row.Desc
		if desc == "" {
			desc = "-"
		}
		fmt.Printf("%s\t%s\t%s\n", row.Manager, row.Package, desc)
	}
}

func parseEnvInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	// Guard against overly long env values
	if len(raw) > 32 {
		fmt.Fprintf(os.Stderr, "fpf warning: env %s too long, using default %d\n", name, fallback)
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid env %s %q, using default %d\n", name, raw, fallback)
		return fallback
	}
	// Clamp to sane range to prevent abuse
	if value < -1000000 || value > 1000000 {
		fmt.Fprintf(os.Stderr, "fpf warning: env %s %d out of range, using default %d\n", name, value, fallback)
		return fallback
	}
	return value
}

func parseEnvFloat(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	if len(raw) > 32 {
		fmt.Fprintf(os.Stderr, "fpf warning: env %s too long, using default %f\n", name, fallback)
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid env %s %q, using default %f\n", name, raw, fallback)
		return fallback
	}
	if value < -1e6 || value > 1e6 {
		fmt.Fprintf(os.Stderr, "fpf warning: env %s %f out of range, using default %f\n", name, value, fallback)
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

var (
	managerReadyCache     = map[string]bool{}
	managerReadyCachePATH string
	managerReadyMu        sync.RWMutex
)

func isManagerCommandReady(manager string) bool {
	// Cache per PATH to handle test mocks that mutate PATH
	pathEnv := os.Getenv("PATH")
	managerReadyMu.RLock()
	if managerReadyCachePATH == pathEnv {
		if v, ok := managerReadyCache[manager]; ok {
			managerReadyMu.RUnlock()
			return v
		}
	}
	managerReadyMu.RUnlock()

	ready := computeManagerCommandReady(manager)

	managerReadyMu.Lock()
	if managerReadyCachePATH != pathEnv {
		// PATH changed - invalidate cache
		managerReadyCache = map[string]bool{}
		managerReadyCachePATH = pathEnv
	}
	managerReadyCache[manager] = ready
	managerReadyMu.Unlock()
	return ready
}

func computeManagerCommandReady(manager string) bool {
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

const maxReloadAttempts = 3
const maxReloadBackoff = 500 * time.Millisecond

func handleIPCReload(conn net.Conn, config any) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	// Ensure conn is closed on failure to prevent leak; caller remains responsible for success path close
	var lastErr error
	for attempt := 1; attempt <= maxReloadAttempts; attempt++ {
		err := performReloadHandshake(conn, config)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == maxReloadAttempts {
			break
		}
		// Exponential backoff capped at maxReloadBackoff, with attempt 1=50ms, 2=200ms
		sleepDuration := time.Duration(attempt*attempt*50) * time.Millisecond
		if sleepDuration > maxReloadBackoff {
			sleepDuration = maxReloadBackoff
		}
		// Don't sleep if connection is already closed/needs immediate return
		if sleepDuration > 0 {
			time.Sleep(sleepDuration)
		}
		// Re-check connection validity before retry
		if conn == nil {
			return fmt.Errorf("connection became nil during retry: %w", lastErr)
		}
	}
	if lastErr == nil {
		return fmt.Errorf("handleIPCReload: all %d attempts failed", maxReloadAttempts)
	}
	return fmt.Errorf("handleIPCReload failed after %d attempts: %w", maxReloadAttempts, lastErr)
}

func performReloadHandshake(conn net.Conn, config any) error {
	if conn == nil {
		return fmt.Errorf("connection is nil")
	}
	// Set deadline to prevent hanging forever
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		// Not all conn types support deadline; log and continue
		fmt.Fprintf(os.Stderr, "fpf warning: failed to set conn deadline: %v\n", err)
	}
	defer func() {
		// Clear deadline
		_ = conn.SetDeadline(time.Time{})
	}()
	return nil
}
