package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIntegrationFeedSearch verifies the core display pipeline
// produces TSV with installed markers and deduping.
func TestIntegrationFeedSearch(t *testing.T) {
	t.Setenv("FPF_CACHE_DIR", t.TempDir())
	t.Setenv("FPF_ENABLE_QUERY_CACHE", "0")
	t.Setenv("FPF_DISABLE_INSTALLED_CACHE", "1")

	// Use synthetic rows to test the display pipeline without needing real managers
	synthetic := []buildDisplayRow{
		{Manager: "apt", Package: "ripgrep", Desc: "fast search"},
		{Manager: "apt", Package: "ripgrep", Desc: "duplicate"},
		{Manager: "brew", Package: "ripgrep", Desc: "brew desc"},
		{Manager: "bun", Package: "ripgrep", Desc: "bun desc"},
	}
	rows := processDisplayRows("ripgrep", []string{"apt", "brew", "bun"}, synthetic)
	if len(rows) == 0 {
		t.Fatal("expected rows, got 0")
	}
	// Should dedupe apt ripgrep
	if len(rows) != 3 {
		t.Fatalf("expected 3 deduped rows, got %d: %+v", len(rows), rows)
	}
	// Check TSV contract
	rendered := renderBuildDisplayRows(rows)
	parsed := parseDisplayRows([]byte(rendered))
	if len(parsed) != len(rows) {
		t.Fatalf("render/parse mismatch: %d vs %d", len(parsed), len(rows))
	}
	for _, r := range parsed {
		if r.Manager == "" || r.Package == "" {
			t.Fatalf("invalid row: %+v", r)
		}
	}
}

// TestIntegrationManagerParsers verifies hardened parsers handle edge cases
func TestIntegrationManagerParsers(t *testing.T) {
	// DNF with arch suffix and extra columns
	dnfOut := "dnfpkg.x86_64 1.0 repo\nsecond.noarch 2.0 repo2\n"
	rows := parseDNFSearch([]byte(dnfOut))
	if len(rows) != 2 || rows[0].Name != "dnfpkg" || rows[1].Name != "second" {
		t.Fatalf("dnf parser failed: %+v", rows)
	}

	// Zypper with minimal columns
	zypperOut := "i | zypkg | package | 1.0 | x86_64 | repo\n"
	rows = parseZypperSearch([]byte(zypperOut))
	if len(rows) != 1 || rows[0].Name != "zypkg" {
		t.Fatalf("zypper parser failed: %+v", rows)
	}

	// Pacman with slash and description on next line
	pacmanOut := "core/pacpkg 1.0\n    Pacman desc\n"
	rows = parsePacmanSearch([]byte(pacmanOut))
	if len(rows) != 1 || rows[0].Name != "pacpkg" {
		t.Fatalf("pacman parser failed: %+v", rows)
	}

	// DNF installed
	installed := parseDnfInstalled([]byte("Installed Packages\ndnfpkg.x86_64 1.0 @repo\n"))
	if len(installed) != 1 || installed[0] != "dnfpkg" {
		t.Fatalf("dnf installed parser failed: %v", installed)
	}

	// Zypper installed
	installed = parseZypperInstalled([]byte("i | x | zypkg | x | 1.0 | x | repo\n"))
	if len(installed) != 1 || installed[0] != "zypkg" {
		t.Fatalf("zypper installed parser failed: %v", installed)
	}
}

// TestIntegrationFlatpakCache verifies cache read/write with timeout handling
func TestIntegrationFlatpakCache(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("FPF_CACHE_DIR", cacheDir)
	t.Setenv("FPF_DISABLE_INSTALLED_CACHE", "0")

	// Simulate installed set
	names := map[string]struct{}{"pkg1": {}, "pkg2": {}}
	storeInstalledSetToCache("brew", names)
	loaded, ok := loadInstalledSetFromCache("brew")
	if !ok || len(loaded) != 2 {
		t.Fatalf("installed cache failed: ok=%v len=%d", ok, len(loaded))
	}

	// Verify timeout for flatpak is now 10s not 0
	timeout := multiManagerSearchTimeout("flatpak", "query", 3)
	if timeout == 0 {
		t.Fatal("flatpak query timeout should not be 0")
	}
}

// TestIntegrationDynamicReload verifies shell quoting and multi-word join
func TestIntegrationDynamicReload(t *testing.T) {
	// Test that parseDynamicReloadRequest joins multi-word queries
	_, query := parseDynamicReloadRequest([]string{"--dynamic-reload", "--", "claude", "code"})
	if query != "claude code" {
		t.Fatalf("expected 'claude code', got %q", query)
	}

	// Test that buildDynamicReloadCommand uses double quotes for {q}
	cmd := buildDynamicReloadCommandGo("apt", "/tmp/fallback.tsv", "apt,bun")
	if !strings.Contains(cmd, "\"{q}\"") {
		t.Fatalf("expected double-quoted {q}, got %q", cmd)
	}
	if strings.Contains(cmd, "'{q}'") {
		t.Fatalf("should not use single quotes for {q}, got %q", cmd)
	}

	// Test flatpak timeout fix
	timeout := multiManagerSearchTimeout("flatpak", "test", 2)
	if timeout == 0 {
		t.Fatal("flatpak timeout should be non-zero")
	}
}

// TestIntegrationPermissions verifies cache dirs use 0o700
func TestIntegrationPermissions(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache-test")
	t.Setenv("FPF_CACHE_DIR", cacheDir)

	names := map[string]struct{}{"testpkg": {}}
	storeInstalledSetToCache("testmgr", names)

	info, err := os.Stat(filepath.Join(cacheDir, "go-installed"))
	if err != nil {
		t.Fatalf("cache dir not created: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("expected 0o700 perms, got %o", info.Mode().Perm())
	}
}
