package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if handled, exitCode := maybeRunGoBuildDisplay(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoMergeDisplay(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoRankDisplay(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunIPCQueryNotifyAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunIPCReloadAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunDynamicReloadAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoInstalledEntries(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunPreviewItemAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoSearchEntries(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := maybeRunGoManagerAction(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	if hasMissingManagerValue(os.Args[1:]) {
		fmt.Fprintln(os.Stderr, "Missing value for --manager")
		os.Exit(1)
	}

	if isVersionRequest(os.Args[1:]) {
		version, err := resolvePackageVersion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "fpf-go: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("fpf %s\n", version)
		return
	}
	os.Exit(runCLI(normalizeManagerArgs(os.Args[1:])))
}

func hasMissingManagerValue(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return false
		}

		switch {
		case arg == "-m" || arg == "--manager":
			if i+1 >= len(args) {
				return true
			}

			next := strings.TrimSpace(args[i+1])
			if next == "" || strings.HasPrefix(next, "-") {
				return true
			}

			i++
		case strings.HasPrefix(arg, "--manager="):
			if strings.TrimSpace(strings.TrimPrefix(arg, "--manager=")) == "" {
				return true
			}
		}
	}

	return false
}

func normalizeManagerArgs(args []string) []string {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			normalized = append(normalized, args[i:]...)
			return normalized
		}

		switch {
		case arg == "-m" || arg == "--manager":
			normalized = append(normalized, arg)
			if i+1 < len(args) {
				next := args[i+1]
				normalized = append(normalized, normalizeManagerName(next))
				i++
			}
		case strings.HasPrefix(arg, "--manager="):
			value := strings.TrimPrefix(arg, "--manager=")
			normalized = append(normalized, "--manager="+normalizeManagerName(value))
		default:
			normalized = append(normalized, arg)
		}
	}

	return normalized
}

func normalizeManagerName(value string) string {
	manager := strings.ToLower(strings.TrimSpace(value))
	// Normalize separators: treat hyphens/underscores as spaces for robust alias handling
	// Collapse multiple whitespace after replacement
	manager = strings.ReplaceAll(manager, "-", " ")
	manager = strings.ReplaceAll(manager, "_", " ")
	manager = strings.Join(strings.Fields(manager), " ")

	switch manager {
	case "homebrew", "brew":
		return "brew"
	case "chocolatey", "chocolate", "choco":
		return "choco"
	case "portage (emerge)", "portage emerge", "portage", "emerge":
		return "emerge"
	case "win get", "winget", "windows package manager", "winget cli":
		return "winget"
	case "scoop":
		return "scoop"
	case "apt get", "apt cache", "apt":
		return "apt"
	case "dnf", "yum", "fedora":
		return "dnf"
	case "pacman", "arch", "yay", "paru":
		return "pacman"
	case "zypper", "opensuse", "suse":
		return "zypper"
	case "flatpak", "flat pak":
		return "flatpak"
	case "snap", "snapd":
		return "snap"
	case "npm", "node", "nodejs":
		return "npm"
	case "bun", "bunjs":
		return "bun"
	default:
		// Remove internal spaces for cases like "win get" already handled, but fallback restores hyphen-free form
		// For generic managers, return compact form without spaces
		compact := strings.ReplaceAll(manager, " ", "")
		// Re-check compact aliases (e.g., "win-get" -> "winget" already via space, but compact handles edge)
		switch compact {
		case "homebrew":
			return "brew"
		case "winget":
			return "winget"
		case "aptget":
			return "apt"
		case "aptcache":
			return "apt"
		default:
			return compact
		}
	}
}

func isVersionRequest(args []string) bool {
	for _, arg := range args {
		if arg == "-v" || arg == "--version" {
			return true
		}
	}
	return false
}

func resolvePackageVersion() (string, error) {
	if version != "" && version != "dev" {
		if len(version) > 64 {
			return "", fmt.Errorf("version too long")
		}
		return version, nil
	}

	pkgPath, err := resolvePackageJSONPath()
	if err != nil {
		return "", err
	}

	raw, err := os.ReadFile(pkgPath)
	if err != nil {
		return "", fmt.Errorf("failed to read package.json at %q: %w", pkgPath, err)
	}
	if len(raw) > 1<<20 {
		return "", fmt.Errorf("package.json too large: %d", len(raw))
	}
	if len(raw) == 0 {
		return "", fmt.Errorf("package.json empty at %q", pkgPath)
	}

	payload := struct {
		Version string `json:"version"`
	}{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("failed to parse package.json at %q: %w", pkgPath, err)
	}
	if payload.Version == "" {
		return "", fmt.Errorf("package.json at %q does not include a version", pkgPath)
	}
	if len(payload.Version) > 64 || strings.Contains(payload.Version, "\n") {
		return "", fmt.Errorf("invalid version in package.json")
	}

	return payload.Version, nil
}

func resolvePackageJSONPath() (string, error) {
	if path := os.Getenv("FPF_PACKAGE_JSON"); path != "" {
		if len(path) > 4096 || strings.Contains(path, "\x00") {
			return "", fmt.Errorf("invalid FPF_PACKAGE_JSON path %q: too long or contains null", path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", fmt.Errorf("invalid FPF_PACKAGE_JSON path %q: %w", path, err)
		}
		if strings.Contains(abs, "..") {
			// Still allow but clean and verify
			abs = filepath.Clean(abs)
		}
		if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("FPF_PACKAGE_JSON %q is symlink", abs)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("package.json not found at %q: %w", abs, err)
		}
		return abs, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot resolve executable path: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		exePath = filepath.Clean(exePath)
	}

	exeDir := filepath.Dir(exePath)
	candidates := []string{
		filepath.Join(exeDir, "package.json"),
		filepath.Join(exeDir, "..", "package.json"),
		filepath.Join(exeDir, "..", "..", "package.json"),
	}

	for _, candidate := range candidates {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}

	cwd, err := os.Getwd()
	if err == nil {
		fallback := filepath.Join(cwd, "package.json")
		if _, err := os.Stat(fallback); err == nil {
			return fallback, nil
		}
	}

	return "", errors.New("unable to locate package.json; set FPF_PACKAGE_JSON to an explicit path")
}

func maybeRunPreviewItemAction(args []string) (bool, int) {
	hasPreview, manager, packageName := parsePreviewRequest(args)
	if !hasPreview {
		return false, 0
	}
	if manager == "" || packageName == "" {
		return true, 0
	}
	if err := validateManagerName(manager); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: invalid manager %q: %v\n", manager, err)
		return true, 1
	}
	if !isValidPkgName(packageName) {
		fmt.Fprintf(os.Stderr, "fpf-go: invalid package name %q\n", packageName)
		return true, 1
	}

	sessionRoot := strings.TrimSpace(os.Getenv("FPF_SESSION_TMP_ROOT"))
	if len(sessionRoot) > 4096 {
		fmt.Fprintf(os.Stderr, "fpf warning: FPF_SESSION_TMP_ROOT too long, falling back\n")
		sessionRoot = filepath.Join(os.TempDir(), "fpf")
	}
	if sessionRoot != "" && !isSafeSessionPath(sessionRoot) {
		fmt.Fprintf(os.Stderr, "fpf warning: invalid FPF_SESSION_TMP_ROOT %q, falling back to %q\n", sessionRoot, filepath.Join(os.TempDir(), "fpf"))
		sessionRoot = filepath.Join(os.TempDir(), "fpf")
	}
	if sessionRoot == "" {
		sessionRoot = os.TempDir()
		if sessionRoot == "" {
			sessionRoot = "/tmp/fpf"
		}
	}
	if info, err := os.Lstat(sessionRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf-go: session path %q is a symlink, refusing to use\n", sessionRoot)
		return true, 1
	}
	if err := os.MkdirAll(sessionRoot, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to create session root: %v\n", err)
		return true, 1
	}
	if info, err := os.Lstat(sessionRoot); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf-go: session path %q is a symlink after creation, refusing to use\n", sessionRoot)
		return true, 1
	}

	cacheDir := filepath.Join(sessionRoot, "preview-cache")
	if info, err := os.Lstat(cacheDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf-go: preview cache dir %q is a symlink, refusing to use\n", cacheDir)
		return true, 1
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to create preview cache dir: %v\n", err)
		return true, 1
	}
	if info, err := os.Lstat(cacheDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf-go: preview cache dir %q is a symlink after creation, refusing to use\n", cacheDir)
		return true, 1
	}

	key := cksumKey(manager + "|" + packageName)
	cacheFile := filepath.Join(cacheDir, fmt.Sprintf("%s.%s.txt", manager, key))

	// Refuse symlinked cache entries (same policy as query/installed caches)
	if info, err := os.Lstat(cacheFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		fmt.Fprintf(os.Stderr, "fpf-go: preview cache file %q is a symlink, ignoring\n", cacheFile)
		_ = os.Remove(cacheFile)
	}

	if raw, err := os.ReadFile(cacheFile); err == nil {
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}

	raw, err := runShowInfoOutput(manager, packageName)
	if err != nil {
		return true, 0
	}

	if len(raw) > 5<<20 {
		fmt.Fprintf(os.Stderr, "fpf-go: preview output too large (%d), truncating\n", len(raw))
		raw = raw[:5<<20]
	}
	if !isSafeCachePath(cacheFile) {
		fmt.Fprintf(os.Stderr, "fpf-go: unsafe cache file path %q\n", cacheFile)
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}
	tmpFile, err := os.CreateTemp(sessionRoot, "preview.")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fpf-go: failed to create preview temp file: %v\n", err)
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		fmt.Fprintf(os.Stderr, "fpf-go: failed to write preview temp file: %v\n", err)
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())
		fmt.Fprintf(os.Stderr, "fpf-go: failed to close preview temp file: %v\n", err)
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}
	if err := os.Rename(tmpFile.Name(), cacheFile); err != nil {
		_ = os.Remove(tmpFile.Name())
		fmt.Fprintf(os.Stderr, "fpf-go: failed to finalize preview cache file: %v\n", err)
		_, _ = os.Stdout.Write(raw)
		return true, 0
	}

	_, _ = os.Stdout.Write(raw)
	return true, 0
}

func parsePreviewRequest(args []string) (bool, string, string) {
	hasPreview := false
	manager := ""
	packageName := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				packageName = args[i+1]
			}
			break
		}
		switch arg {
		case "--preview-item":
			hasPreview = true
		case "--manager":
			if i+1 < len(args) {
				manager = normalizeManagerName(args[i+1])
				i++
			}
		}
	}

	return hasPreview, manager, packageName
}

func cksumKey(input string) string {
	return stableChecksum(input)
}

func runShowInfoOutput(manager string, packageName string) ([]byte, error) {
	if err := validateManagerName(manager); err != nil {
		return nil, err
	}
	if !isValidPkgName(packageName) {
		return nil, fmt.Errorf("invalid package name: %q", packageName)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx,
		os.Args[0],
		"--go-manager-action", "show_info",
		"--go-manager", manager,
		"--", packageName,
	)
	cmd.Env = append(os.Environ(), "FPF_USE_GO_MANAGER_ACTIONS=1")
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, context.DeadlineExceeded
	}
	return out, err
}
