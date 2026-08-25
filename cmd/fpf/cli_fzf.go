package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// fzfLocalDir returns the per-user directory path where fpf places a bundled fzf binary.
// It returns an empty string if the user's home directory cannot be determined.
func fzfLocalDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "fpf", "fzf")
}

// localFzfBinaryPath returns the path to the per-user fzf binary in the fpf local directory if the file exists, is not a directory, and—on non-Windows systems—has execute permission; otherwise it returns an empty string.
// On Windows it looks for `fzf.exe` and does not check execute permission.
func localFzfBinaryPath() string {
	if strings.TrimSpace(os.Getenv("FPF_DISABLE_LOCAL_FZF")) == "1" {
		return ""
	}
	if isTestFzfMockActive() {
		return ""
	}
	dir := fzfLocalDir()
	if dir == "" {
		return ""
	}
	binaryName := "fzf"
	if runtime.GOOS == "windows" {
		binaryName = "fzf.exe"
	}
	p := filepath.Join(dir, binaryName)
	info, err := os.Stat(p)
	if err != nil || info.IsDir() {
		return ""
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		return ""
	}
	return p
}

func isTestFzfMockActive() bool {
	if strings.TrimSpace(os.Getenv("FPF_TEST_MOCK_BIN")) != "" {
		return true
	}
	if strings.TrimSpace(os.Getenv("FPF_TEST_FORCE_FZF_MISSING")) == "1" {
		return true
	}
	if strings.TrimSpace(os.Getenv("FPF_TEST_FIXTURES")) == "1" {
		return true
	}
	// Heuristic for Go unit tests that use a temp mock PATH without setting FPF_TEST_MOCK_BIN:
	// if exec.LookPath finds fzf under /tmp, prefer the mock over local.
	if path, err := exec.LookPath("fzf"); err == nil {
		if strings.HasPrefix(path, "/tmp/") || strings.Contains(path, "/T/") {
			return true
		}
	}
	return false
}

// resolveFzfBinaryPath returns the path to the fzf executable, preferring a local per-user installation when available and falling back to the system "fzf" command.
func resolveFzfBinaryPath() string {
	if override := strings.TrimSpace(os.Getenv("FPF_FZF_BINARY")); override != "" {
		return override
	}
	if mockBin := strings.TrimSpace(os.Getenv("FPF_TEST_MOCK_BIN")); mockBin != "" {
		candidate := filepath.Join(mockBin, "fzf")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if runtime.GOOS == "windows" || info.Mode()&0o111 != 0 {
				return candidate
			}
		}
	}
	if p := localFzfBinaryPath(); p != "" {
		return p
	}
	return "fzf"
}

// fzfCommandAvailableGo reports whether an fzf executable is available for use.
// It returns true if a usable fzf binary is found via test overrides, the local fzf install, or the system PATH.
func fzfCommandAvailableGo() bool {
	if strings.TrimSpace(os.Getenv("FPF_TEST_FORCE_FZF_MISSING")) == "1" {
		mockBin := strings.TrimSpace(os.Getenv("FPF_TEST_MOCK_BIN"))
		if mockBin != "" {
			candidate := filepath.Join(mockBin, "fzf")
			if info, err := os.Stat(candidate); err == nil && info.Mode()&0o111 != 0 {
				return true
			}
		}
		return false
	}
	if localFzfBinaryPath() != "" {
		return true
	}
	return commandExistsGo("fzf")
}

func managerCanInstallFzfGo(manager string) bool {
	switch manager {
	case "apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap":
		return true
	default:
		return false
	}
}

// installFzfWithManagerGo installs the fzf executable using the specified package manager.
//
// If the environment variable FPF_TEST_FZF_MANAGER_INSTALL_FAIL is set to "1", the function
// returns a forced failure error for testing. For supported managers it runs the manager's
// install command and returns any error produced; for an unsupported manager it returns an
// error indicating installation is not supported.
func installFzfWithManagerGo(manager string) error {
	if strings.TrimSpace(os.Getenv("FPF_TEST_FZF_MANAGER_INSTALL_FAIL")) == "1" {
		return fmt.Errorf("forced manager install failure")
	}
	switch manager {
	case "apt":
		return runRootCommand("apt-get", "install", "-y", "--", "fzf")
	case "dnf":
		return runRootCommand("dnf", "install", "-y", "--", "fzf")
	case "pacman":
		return runRootCommand("pacman", "-S", "--needed", "--", "fzf")
	case "zypper":
		return runRootCommand("zypper", "--non-interactive", "install", "--auto-agree-with-licenses", "--", "fzf")
	case "emerge":
		return runRootCommand("emerge", "--ask=n", "app-shells/fzf")
	case "brew":
		return runCommand("brew", "install", "fzf")
	case "winget":
		if err := runCommand("winget", "install", "--id", "junegunn.fzf", "--exact", "--source", "winget", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"); err != nil {
			return runCommand("winget", "install", "--id", "fzf", "--exact", "--source", "winget", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
		}
		return nil
	case "choco":
		return runCommand("choco", "install", "fzf", "-y")
	case "scoop":
		return runCommand("scoop", "install", "fzf")
	case "snap":
		return runRootCommand("snap", "install", "fzf")
	default:
		return fmt.Errorf("unsupported manager for fzf install")
	}
}

// installFzfFromReleaseFallbackGo attempts to install fzf into the per-user fpf fzf directory by downloading and extracting the latest GitHub release for the current OS (supports linux, darwin, windows). In test mode (FPF_TEST_BOOTSTRAP_FZF_FALLBACK=1) it instead creates a symlink from a provided mock command into the mock bin.
// It returns true if the installation produced an executable fzf binary at the expected local path, false on any failure.
func installFzfFromReleaseFallbackGo() bool {
	if strings.TrimSpace(os.Getenv("FPF_TEST_BOOTSTRAP_FZF_FALLBACK")) == "1" {
		mockCmdPath := strings.TrimSpace(os.Getenv("FPF_TEST_MOCKCMD_PATH"))
		mockBin := strings.TrimSpace(os.Getenv("FPF_TEST_MOCK_BIN"))
		if mockCmdPath == "" || mockBin == "" {
			return false
		}
		target := filepath.Join(mockBin, "fzf")
		_ = os.Remove(target)
		if err := os.Symlink(mockCmdPath, target); err != nil {
			return false
		}
		return true
	}

	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goos != "linux" && goos != "darwin" && goos != "windows" {
		return false
	}

	dir := fzfLocalDir()
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}

	version := resolveLatestFzfVersion()
	if version == "" {
		return false
	}
	if matched, _ := regexp.MatchString(`^[0-9]+\.[0-9]+\.[0-9]+$`, version); !matched {
		return false
	}

	// Map GOARCH to fzf's expected arch strings
	fzfArch := goarch
	switch goarch {
	case "arm":
		fzfArch = "armv7" // fzf uses armv7 for 32-bit ARM
	case "amd64":
		fzfArch = "amd64"
	case "386":
		fzfArch = "386"
	case "arm64":
		fzfArch = "arm64"
	}

	binaryName := "fzf"
	if goos == "windows" {
		binaryName = "fzf.exe"
	}
	target := filepath.Join(dir, binaryName)

	// fzf asset name format: fzf-{version}-{os}_{arch}.tar.gz (or .zip on Windows)
	var url string
	if goos == "windows" {
		url = fmt.Sprintf("https://github.com/junegunn/fzf/releases/download/v%s/fzf-%s-%s_%s.zip", version, version, goos, fzfArch)
	} else {
		url = fmt.Sprintf("https://github.com/junegunn/fzf/releases/download/v%s/fzf-%s-%s_%s.tar.gz", version, version, goos, fzfArch)
	}

	if err := downloadAndInstallFzf(url, target); err != nil {
		_ = os.Remove(target)
		return false
	}

	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return false
	}
	if goos != "windows" && info.Mode()&0o111 == 0 {
		return false
	}
	return true
}

// resolveLatestFzfVersion retrieves the latest fzf release version by parsing the redirect target of GitHub's releases/latest URL.
// It returns the version string without the leading "v" (for example, "0.71.0"), or an empty string if the request fails or the redirect format is unexpected.
func resolveLatestFzfVersion() string {
	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequest("GET", "https://github.com/junegunn/fzf/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "fpf-cli")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" {
		return ""
	}
	// Location looks like: .../releases/tag/v0.71.0
	idx := strings.LastIndex(loc, "/v")
	if idx < 0 {
		return ""
	}
	version := loc[idx+2:]
	if matched, _ := regexp.MatchString(`^[0-9]+\.[0-9]+\.[0-9]+$`, version); !matched {
		return ""
	}
	return version
}

// fetchFzfChecksum retrieves the expected SHA256 checksum for the given fzf asset filename.
// It downloads the checksums file from GitHub releases and parses it to find the matching entry.
// Returns the hex-encoded checksum or an error if the file cannot be fetched or parsed.
func fetchFzfChecksum(version, assetFilename string) (string, error) {
	if version == "" || assetFilename == "" {
		return "", fmt.Errorf("version and asset filename required")
	}
	if matched, _ := regexp.MatchString(`^[0-9]+\.[0-9]+\.[0-9]+$`, version); !matched {
		return "", fmt.Errorf("invalid version: %q", version)
	}
	if len(assetFilename) > 256 || strings.Contains(assetFilename, "..") {
		return "", fmt.Errorf("invalid asset filename: %q", assetFilename)
	}
	client := &http.Client{Timeout: 30 * time.Second}

	// Try known checksum file naming conventions
	for _, pattern := range []string{
		fmt.Sprintf("https://github.com/junegunn/fzf/releases/download/v%s/fzf_%s_checksums.txt", version, version),
		fmt.Sprintf("https://github.com/junegunn/fzf/releases/download/v%s/fzf-%s-checksums.txt", version, version),
	} {
		req, err := http.NewRequest("GET", pattern, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "fpf-cli")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 && parts[1] == assetFilename {
				// Validate checksum format (64 hex chars)
				if matched, _ := regexp.MatchString(`^[a-fA-F0-9]{64}$`, parts[0]); !matched {
					continue
				}
				return parts[0], nil
			}
		}
	}
	return "", fmt.Errorf("checksum not found for %s", assetFilename)
}

// downloadAndInstallFzf downloads the archive at the given URL, verifies its SHA256 checksum,
// and installs the contained fzf binary to target.
// The function performs an HTTP GET and returns an error for network failures or non-200 responses.
// It fetches the expected checksum, downloads the asset into memory, computes and verifies the SHA256,
// and then extracts the binary. If the URL ends with ".zip" the archive is extracted with extractZipFzf;
// otherwise it is treated as a tar.gz and extracted with extractTarGzFzf.
// Any error from download, checksum verification, extraction or write operation is returned.
func downloadAndInstallFzf(url, target string) error {
	if url == "" || target == "" {
		return fmt.Errorf("url and target required")
	}
	if len(url) > 2048 || len(target) > 4096 {
		return fmt.Errorf("url or target too long")
	}
	if !strings.HasPrefix(url, "https://github.com/junegunn/fzf/releases/download/") {
		return fmt.Errorf("invalid URL origin")
	}
	// Extract version and asset filename from URL
	parts := strings.Split(url, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid URL format")
	}
	assetFilename := parts[len(parts)-1]
	versionTag := parts[len(parts)-2]
	version := strings.TrimPrefix(versionTag, "v")
	if version == "" {
		return fmt.Errorf("invalid version in URL")
	}

	expectedChecksum, err := fetchFzfChecksum(version, assetFilename)
	if err != nil {
		return fmt.Errorf("failed to fetch checksum: %w", err)
	}

	// Download asset into memory
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "fpf-cli")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
	if err != nil {
		return fmt.Errorf("failed to download asset: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("empty download for %s", url)
	}
	if len(data) > 50<<20 {
		return fmt.Errorf("asset too large: %d", len(data))
	}

	hash := sha256.Sum256(data)
	actualChecksum := hex.EncodeToString(hash[:])
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	binaryName := filepath.Base(target)

	reader := bytes.NewReader(data)
	if strings.HasSuffix(url, ".zip") {
		return extractZipFzf(reader, target, binaryName)
	}
	return extractTarGzFzf(reader, target)
}

// extractTarGzFzf extracts the first regular file named "fzf" from a gzip-compressed tar stream and writes it to target as an executable.
// It returns an error if the gzip or tar stream cannot be read, if writing the extracted file fails, or if no matching "fzf" file is found in the archive.
func extractTarGzFzf(reader io.Reader, target string) error {
	gz, err := gzip.NewReader(reader)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if filepath.Base(hdr.Name) == "fzf" && hdr.Typeflag == tar.TypeReg {
			return writeExecutable(target, tr)
		}
	}
	return fmt.Errorf("fzf binary not found in archive")
}

// extractZipFzf extracts the entry whose basename equals binaryName from the provided ZIP stream
// and writes it as an executable at target using writeExecutable.
// It returns an error if the archive cannot be read, the named binary is not present, or writing the executable fails.
func extractZipFzf(reader io.Reader, target, binaryName string) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == binaryName && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			return writeExecutable(target, rc)
		}
	}
	return fmt.Errorf("%s not found in archive", binaryName)
}

// writeExecutable writes data from reader to target file atomically and makes the file executable.
// It creates a unique temporary file in the target directory with mode 0755, writes the contents,
// closes it, and renames it into place. On any failure the temporary file is cleaned up; any error is returned.
func writeExecutable(target string, reader io.Reader) error {
	if target == "" || reader == nil {
		return fmt.Errorf("target and reader required")
	}
	if len(target) > 4096 {
		return fmt.Errorf("target path too long")
	}
	if !filepath.IsAbs(target) && !strings.HasPrefix(target, os.TempDir()) {
		// Allow relative only if under safe path; otherwise require absolute
		if strings.Contains(target, "..") {
			return fmt.Errorf("invalid target path: %q", target)
		}
	}
	targetDir := filepath.Dir(target)
	targetBase := filepath.Base(target)
	if targetBase == "" || targetBase == "." || strings.Contains(targetBase, "/") {
		return fmt.Errorf("invalid target base: %q", targetBase)
	}
	if info, err := os.Lstat(targetDir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("target dir %q is symlink", targetDir)
	}

	// Create a unique temp file in the target directory
	f, err := os.CreateTemp(targetDir, targetBase+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp for %q: %w", target, err)
	}
	tmpPath := f.Name()

	// Ensure cleanup on error
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()

	// Set executable permissions
	if err := f.Chmod(0o755); err != nil {
		f.Close()
		return fmt.Errorf("chmod temp %q: %w", tmpPath, err)
	}

	// Write data with limit to prevent OOM / disk fill
	limited := io.LimitReader(reader, 100<<20)
	if _, err := io.Copy(f, limited); err != nil {
		f.Close()
		return fmt.Errorf("write temp %q: %w", tmpPath, err)
	}

	// Close and check for errors
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temp %q: %w", tmpPath, err)
	}

	// Verify temp file exists and not symlink
	if info, err := os.Lstat(tmpPath); err != nil {
		return fmt.Errorf("stat temp %q: %w", tmpPath, err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("temp file is symlink: %q", tmpPath)
	}

	// Atomically rename to target
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("rename %q -> %q: %w", tmpPath, target, err)
	}

	// Success - prevent cleanup of temp file
	tmpPath = ""
	return nil
}

// buildFzfBootstrapCandidatesGo builds an ordered list of package managers suitable for bootstrapping fzf.
// It filters the provided managers to those that can install fzf and are command-ready, preserves the
// first-seen order while deduplicating, and if none remain returns a fallback list of common managers.
func buildFzfBootstrapCandidatesGo(managers []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	add := func(manager string) {
		if manager == "" || !managerCanInstallFzfGo(manager) || !isManagerCommandReady(manager) {
			return
		}
		if _, ok := seen[manager]; ok {
			return
		}
		seen[manager] = struct{}{}
		out = append(out, manager)
	}
	for _, m := range managers {
		add(m)
	}
	if len(out) > 0 {
		return out
	}
	for _, m := range []string{"apt", "dnf", "pacman", "zypper", "emerge", "brew", "winget", "choco", "scoop", "snap"} {
		add(m)
	}
	return out
}

// ensureFzfGo ensures a usable fzf binary is available and that its version
// meets the minimum required for reliable live search.
//
// It tries to auto-install fzf by attempting package-manager installs using
// the ordered candidate list derived from the provided managers, and if that
// fails falls back to downloading and installing a release into the local
// fzf directory. If fzf is present but older than the minimum required
// version, it will attempt the same upgrade steps.
//
// The managers parameter supplies preferred package-manager names (order is
// respected) to use when attempting bootstrap or upgrade operations.
//
// Returns an error when fzf cannot be installed (package-manager attempts
// and release fallback both fail), or when an existing fzf installation is
// outdated and cannot be upgraded to the minimum required version.
func ensureFzfGo(managers []string) error {
	if !fzfCommandAvailableGo() {
		candidates := buildFzfBootstrapCandidatesGo(managers)
		if len(candidates) == 0 {
			return fmt.Errorf("fzf is required and no compatible manager is available to auto-install it")
		}
		fmt.Fprintf(os.Stderr, "fzf is missing. Auto-installing with: %s\n", joinManagerLabelsGo(candidates))
		for _, manager := range candidates {
			fmt.Fprintf(os.Stderr, "Attempting fzf install with %s\n", managerLabelGo(manager))
			if err := installFzfWithManagerGo(manager); err == nil && fzfCommandAvailableGo() {
				break
			}
		}
		if !fzfCommandAvailableGo() {
			fmt.Fprintln(os.Stderr, "Package-manager bootstrap did not provide fzf. Trying release binary fallback.")
			if !installFzfFromReleaseFallbackGo() || !fzfCommandAvailableGo() {
				return fmt.Errorf("Failed to auto-install fzf. Install fzf manually and rerun.")
			}
		}
	}

	if !checkFzfVersionMin(fzfMinVersionChangeReload) {
		fmt.Fprintf(os.Stderr, "fzf is outdated (need >= %s for reliable live search). Upgrading...\n", fzfMinVersionChangeReload)
		upgraded := false
		for _, manager := range buildFzfBootstrapCandidatesGo(managers) {
			if err := installFzfWithManagerGo(manager); err == nil && checkFzfVersionMin(fzfMinVersionChangeReload) {
				upgraded = true
				break
			}
		}
		if !upgraded {
			fmt.Fprintln(os.Stderr, "Package manager did not provide a recent fzf. Downloading latest release...")
			if installFzfFromReleaseFallbackGo() && localFzfBinaryPath() != "" && checkFzfVersionMin(fzfMinVersionChangeReload) {
				upgraded = true
			}
		}
		if !upgraded {
			fmt.Fprintf(os.Stderr, "Error: fzf < %s has a known race condition with live search.\n", fzfMinVersionChangeReload)
			fmt.Fprintln(os.Stderr, "Live updates cannot be enabled. Please upgrade fzf manually.")
			return fmt.Errorf("fzf version %s or higher is required", fzfMinVersionChangeReload)
		}
	}
	return nil
}

// It checks `fzf --help` for a `--listen` entry and ensures the fzf version is >= 0.56.1.
func fzfSupportsListenGo() bool {
	// Check for --listen flag support first with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, resolveFzfBinaryPath(), "--help")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintln(os.Stderr, "fpf warning: fzf --help timed out")
		}
		return false
	}
	if !strings.Contains(string(out), "--listen") {
		return false
	}
	// change:reload: requires fzf >= 0.56.1
	return checkFzfVersionMin("0.56.1")
}

// checkFzfVersionMin reports whether the installed fzf binary's version is greater than or equal to minVersion.
// It returns false if the fzf executable cannot be run or its version cannot be determined.
func checkFzfVersionMin(minVersion string) bool {
	if minVersion == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, resolveFzfBinaryPath(), "--version")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintln(os.Stderr, "fpf warning: fzf --version timed out")
		}
		return false
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return false
	}
	// fzf --version outputs just the version number like "0.48.0"
	version = strings.Split(version, " ")[0]
	if version == "" {
		return false
	}
	return compareVersions(version, minVersion) >= 0
}

func compareVersions(v1, v2 string) int {
	parts1 := parseVersion(v1)
	parts2 := parseVersion(v2)
	for i := 0; i < len(parts1) || i < len(parts2); i++ {
		p1 := 0
		p2 := 0
		if i < len(parts1) {
			p1 = parts1[i]
		}
		if i < len(parts2) {
			p2 = parts2[i]
		}
		if p1 < p2 {
			return -1
		}
		if p1 > p2 {
			return 1
		}
	}
	return 0
}

// parseVersion parses a dot-separated version string into a slice of integers.
// It splits v on '.' and converts each segment to an int, stopping at the first
// segment that is not a valid integer. Returns the sequence of parsed integers
// (which may be empty).
func parseVersion(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		num, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		result = append(result, num)
	}
	return result
}

// fzfSupportsResultBindGo reports whether the installed fzf supports the `result` binding for `--bind`.
// It returns true if the `result` key is supported, false otherwise.
func fzfSupportsResultBindGo() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, resolveFzfBinaryPath(), "--bind=result:abort", "--filter", "probe")
	cmd.Stdin = strings.NewReader("probe\n")
	out, _ := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		fmt.Fprintln(os.Stderr, "fpf warning: fzf --bind check timed out")
		return false
	}
	return !strings.Contains(string(out), "unsupported key: result")
}

// runFuzzySelectorGo runs an fzf subprocess configured with preview, keybinds, and optional dynamic reload bindings and returns the raw selection.
// It returns the stdout produced by fzf on success; returns an empty string and nil when the user cancels (fzf exit code 1 or 130); returns a non-nil error for other failures.
// The excludeSlowManagers parameter indicates whether flatpak/npm are excluded from live search (meaning Ctrl+R is needed for full search).
func runFuzzySelectorGo(query, inputFile, header, helpFile, keybindFile, reloadCmd, reloadFullCmd, reloadIPCCmd, sessionTmp string, excludeSlowManagers bool) (string, error) {
	stageStart := time.Now()
	defer logPerfTraceStage("fzf", stageStart)

	if len(query) > maxQueryLength {
		query = query[:maxQueryLength]
	}
	if inputFile == "" {
		return "", fmt.Errorf("inputFile required")
	}
	if info, err := os.Lstat(inputFile); err != nil {
		return "", fmt.Errorf("input file %q: %w", inputFile, err)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("input file is symlink: %q", inputFile)
	}
	if header != "" && len(header) > 4096 {
		header = header[:4096]
	}
	if sessionTmp == "" {
		return "", fmt.Errorf("sessionTmp required")
	}
	if !isSafeSessionPath(sessionTmp) {
		fmt.Fprintf(os.Stderr, "fpf warning: sessionTmp %q not safe, using TempDir\n", sessionTmp)
		sessionTmp = filepath.Join(os.TempDir(), "fpf")
	}

	scriptPath := os.Args[0]
	if scriptPath == "" {
		return "", fmt.Errorf("cannot determine executable path")
	}
	previewCmd := fmt.Sprintf("FPF_SESSION_TMP_ROOT=%s %s --preview-item --manager {1} -- {2}", shellQuote(sessionTmp), shellQuote(scriptPath))

	prompt := "Search> "
	if excludeSlowManagers {
		prompt = "Search (fast)> "
	}

	args := []string{
		"-q", query,
		"-m",
		"-e",
		"--delimiter=\t",
		"--with-nth=1,2,3",
		"--preview=" + previewCmd,
		"--preview-window=55%:wrap:border-sharp",
		"--layout=reverse",
		"--marker=>>",
		"--prompt=" + prompt,
		"--header=" + header,
		"--info=inline",
		"--margin=2%,1%,2%,1%",
		"--cycle",
		"--tiebreak=begin,chunk,length",
		"--bind=ctrl-k:preview:cat " + shellQuote(keybindFile),
		"--bind=ctrl-h:preview:cat " + shellQuote(helpFile),
		"--bind=ctrl-/:change-preview-window(hidden|)",
		"--bind=ctrl-n:next-selected,ctrl-b:prev-selected",
		"--bind=focus:transform-preview-label:echo [{1}] {2}",
	}

	ctrlRReload := reloadCmd
	if reloadFullCmd != "" {
		ctrlRReload = reloadFullCmd
	}

	// Determine the correct prompt to restore after reload completes
	reloadResultPrompt := "Search> "
	if excludeSlowManagers {
		reloadResultPrompt = "Search (fast)> "
	}

	if reloadIPCCmd != "" {
		args = append(args, "--listen=0")
		args = append(args, "--bind=change:execute-silent:"+reloadIPCCmd)
		if ctrlRReload != "" {
			if fzfSupportsResultBindGo() {
				args = append(args, "--bind=ctrl-r:change-prompt(Loading> )+reload:"+ctrlRReload)
				args = append(args, "--bind=result:change-prompt("+reloadResultPrompt+")")
			} else {
				args = append(args, "--bind=ctrl-r:reload:"+ctrlRReload)
			}
		}
	} else if reloadCmd != "" {
		if fzfSupportsResultBindGo() {
			args = append(args, "--bind=change:change-prompt(Loading> )+reload:"+reloadCmd)
			args = append(args, "--bind=ctrl-r:change-prompt(Loading> )+reload:"+ctrlRReload)
			args = append(args, "--bind=result:change-prompt("+reloadResultPrompt+")")
		} else {
			args = append(args, "--bind=change:reload:"+reloadCmd)
			args = append(args, "--bind=ctrl-r:reload:"+ctrlRReload)
		}
	}

	stdinFile, err := os.Open(inputFile)
	if err != nil {
		return "", err
	}
	defer stdinFile.Close()

	fzfBin := resolveFzfBinaryPath()
	cmd := exec.Command(fzfBin, args...)
	cmd.Env = append(os.Environ(), "SHELL=bash")
	cmd.Stdin = stdinFile
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	useDirectStderr := stderrHasTerminalGo()
	var stderr bytes.Buffer
	if useDirectStderr {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = &stderr
	}

	err = cmd.Run()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			return "", err
		}
		switch exitErr.ExitCode() {
		case 1, 130:
			return "", nil
		}
		if !useDirectStderr {
			stderrText := strings.TrimSpace(stderr.String())
			if stderrText != "" {
				return "", fmt.Errorf("fzf exited with code %d: %s", exitErr.ExitCode(), stderrText)
			}
		}
		return "", fmt.Errorf("fzf exited with code %d", exitErr.ExitCode())
	}
	return stdout.String(), nil
}

func stderrHasTerminalGo() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func buildDynamicReloadCommandGo(managerOverride, fallbackFile, managerListCSV string) string {
	bypass := dynamicReloadBypassValueGo()
	parts := []string{
		"FPF_SKIP_INSTALLED_MARKERS=1",
		"FPF_BYPASS_QUERY_CACHE=" + bypass,
		"FPF_SKIP_QUERY_CACHE_WRITE=1",
		"FPF_IPC_MANAGER_OVERRIDE=" + shellQuoteIfNeeded(managerOverride),
		"FPF_IPC_MANAGER_LIST=" + shellQuoteIfNeeded(managerListCSV),
		"FPF_IPC_FALLBACK_FILE=" + shellQuoteIfNeeded(fallbackFile),
		shellQuote(os.Args[0]),
		"--dynamic-reload",
		"--",
		"\"{q}\"",
	}
	return strings.Join(parts, " ")
}

func buildDynamicQueryNotifyIPCCommandGo(managerOverride, fallbackFile, managerListCSV string) string {
	parts := []string{
		"FPF_FZF_LISTEN_HOST=localhost:$FZF_PORT",
		"FPF_IPC_MANAGER_OVERRIDE=" + shellQuoteIfNeeded(managerOverride),
		"FPF_IPC_MANAGER_LIST=" + shellQuoteIfNeeded(managerListCSV),
		"FPF_IPC_FALLBACK_FILE=" + shellQuoteIfNeeded(fallbackFile),
		shellQuote(os.Args[0]),
		"--ipc-query-notify",
		"--",
		"\"{q}\"",
	}
	return strings.Join(parts, " ")
}

func shellQuoteIfNeeded(value string) string {
	if value == "" {
		return "''"
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '_', '-', '.', '/', ':', ',':
			continue
		}
		return shellQuote(value)
	}
	return value
}

func dynamicReloadBypassValueGo() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD_BYPASS_QUERY_CACHE")))
	switch v {
	case "0", "false", "no", "off":
		return "0"
	default:
		return "1"
	}
}

func dynamicReloadManagers(managers []string) []string {
	defaultManagers := defaultDynamicReloadManagers(managers)
	override := strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD_MANAGERS"))
	if override != "" {
		if strings.EqualFold(override, "all") {
			return managers
		}
		requested := splitManagerArg(override)
		if len(requested) == 0 {
			return defaultManagers
		}
		available := make(map[string]struct{}, len(managers))
		for _, manager := range managers {
			available[manager] = struct{}{}
		}
		filtered := make([]string, 0, len(requested))
		for _, manager := range requested {
			if _, ok := available[manager]; ok {
				filtered = append(filtered, manager)
			}
		}
		if len(filtered) > 0 {
			return filtered
		}
		return defaultManagers
	}

	return defaultManagers
}

func defaultDynamicReloadManagers(managers []string) []string {

	slow := map[string]struct{}{
		"npm": {},
	}
	fast := make([]string, 0, len(managers))
	for _, manager := range managers {
		if _, isSlow := slow[manager]; !isSlow {
			fast = append(fast, manager)
		}
	}
	if len(fast) == 0 {
		return managers
	}
	return fast
}

func dynamicReloadEnabledGo(managerCount int) bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD")))
	switch mode {
	case "", "always", "auto", "on", "1", "true", "yes":
		return true
	case "never", "off", "0", "false", "no":
		return false
	case "single":
		return managerCount == 1
	default:
		return true
	}
}

func dynamicReloadUseIPCGo() bool {
	transport := strings.ToLower(strings.TrimSpace(os.Getenv("FPF_DYNAMIC_RELOAD_TRANSPORT")))
	switch transport {
	case "ipc", "listen", "http", "auto":
		return fzfSupportsListenGo()
	default:
		return false
	}
}
