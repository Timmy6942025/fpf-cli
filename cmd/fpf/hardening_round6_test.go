package main

import (
	"compress/gzip"

	"github.com/Timmy6942025/fpf-cli/internal/flatpak"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEnvIntClamped(t *testing.T) {
	t.Setenv("FPF_T_TEST", "abc")
	if got := parseEnvIntClamped("FPF_T_TEST", 7, 0, 100); got != 7 {
		t.Fatalf("invalid value should fall back: got %d", got)
	}
	t.Setenv("FPF_T_TEST", "500")
	if got := parseEnvIntClamped("FPF_T_TEST", 7, 0, 100); got != 100 {
		t.Fatalf("over-max should clamp: got %d", got)
	}
	t.Setenv("FPF_T_TEST", "-5")
	if got := parseEnvIntClamped("FPF_T_TEST", 7, 0, 100); got != 0 {
		t.Fatalf("under-min should clamp to min: got %d", got)
	}
	t.Setenv("FPF_T_TEST", "")
	if got := parseEnvIntClamped("FPF_T_TEST", 7, 0, 100); got != 7 {
		t.Fatalf("unset should fall back: got %d", got)
	}
}

func TestParseStrictIntAndClampInt(t *testing.T) {
	if got := parseStrictInt("42"); got != 42 {
		t.Fatalf("parseStrictInt(42)=%d", got)
	}
	if got := parseStrictInt("5junk"); got != -1 {
		t.Fatalf("trailing junk must fail: got %d", got)
	}
	if got := clampInt(-3, 0, 10); got != 0 {
		t.Fatalf("clampInt low=%d", got)
	}
	if got := clampInt(99, 0, 10); got != 10 {
		t.Fatalf("clampInt high=%d", got)
	}
}

// Regression: DNF arch strip must only remove known RPM arches.
func TestDNFArchStripKnownOnly(t *testing.T) {
	rows := parseDNFSearch([]byte("my.pkg.name 1.0 repo\nreal.noarch 2.0 repo\n"))
	if len(rows) != 2 {
		t.Fatalf("len=%d", len(rows))
	}
	if rows[0].Name != "my.pkg.name" {
		t.Fatalf("dotted name corrupted: %q", rows[0].Name)
	}
	if rows[1].Name != "real" {
		t.Fatalf("known arch not stripped: %q", rows[1].Name)
	}
}

// Regression: installed variant of the same arch-strip logic.
func TestDnfInstalledArchStripKnownOnly(t *testing.T) {
	names := parseDnfInstalled([]byte("Installed Packages\nmy.pkg.name 1.0 @repo\nkernel.x86_64 6.0 @updates\n"))
	if len(names) != 2 || names[0] != "my.pkg.name" || names[1] != "kernel" {
		t.Fatalf("names=%v", names)
	}
}

// Regression: flatpak parser handles both compressed and uncompressed appstream.
func TestFlatpakParserUncompressedAndGzip(t *testing.T) {
	xml := `<?xml version="1.0"?><components origin="flathub"><component type="desktop-application"><id>org.example.T</id><name>T</name><summary>S</summary></component></components>`
	dir := t.TempDir()
	plain := filepath.Join(dir, "appstream.xml")
	if err := os.WriteFile(plain, []byte(xml), 0o600); err != nil {
		t.Fatal(err)
	}
	apps, err := flatpak.ParseAppStreamFile(plain)
	if err != nil || len(apps) != 1 || apps[0].ID != "org.example.T" {
		t.Fatalf("uncompressed parse failed: %v %+v", err, apps)
	}

	gzPath := filepath.Join(dir, "appstream.xml.gz")
	f, _ := os.Create(gzPath)
	w := gzip.NewWriter(f)
	_, _ = w.Write([]byte(xml))
	_ = w.Close()
	_ = f.Close()
	apps, err = flatpak.ParseAppStreamFile(gzPath)
	if err != nil || len(apps) != 1 {
		t.Fatalf("gzip parse failed: %v", err)
	}
}

// Regression: XML entity expansion is disabled (billion-laughs defense).
func TestFlatpakParserRejectsEntities(t *testing.T) {
	bomb := `<?xml version="1.0"?><!DOCTYPE components [<!ENTITY a "aaaa">]><components origin="x">&a;</components>`
	dir := t.TempDir()
	p := filepath.Join(dir, "bomb.xml")
	if err := os.WriteFile(p, []byte(bomb), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := flatpak.ParseAppStreamFile(p); err == nil {
		t.Fatal("expected entity declaration to be rejected")
	}
}

// Round-5 fix: merge treats read failure (non-missing file) as error.
func TestMergeDisplayReadFailureIsError(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.tsv")
	// A directory as source triggers a read error that is not IsNotExist
	in := filepath.Join(dir, "somedir")
	if err := os.Mkdir(in, 0o700); err != nil {
		t.Fatal(err)
	}
	err := runMergeDisplay(mergeInput{SourceFile: in, OutputFile: out})
	if err == nil {
		t.Fatal("expected read failure on directory source to be an error")
	}
	if !strings.Contains(err.Error(), "read source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Round-5 fix: show_info without packages is rejected.
func TestShowInfoRequiresPackage(t *testing.T) {
	err := executeManagerAction(managerActionInput{Action: "show_info", Manager: "apt"})
	if err == nil || !strings.Contains(err.Error(), "show_info requires a package") {
		t.Fatalf("expected empty-pkg show_info rejection, got %v", err)
	}
}

// Round-5 fix: IPC host parsing validates numeric ports.
func TestSendFzfListenActionValidatesPort(t *testing.T) {
	t.Setenv("FPF_FZF_LISTEN_HOST", "localhost:notaport")
	fallback := filepath.Join(t.TempDir(), "fb.tsv")
	if err := os.WriteFile(fallback, []byte("apt\tp\t-\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FPF_IPC_FALLBACK_FILE", fallback)
	err := runIPCReload("query")
	if err == nil || !strings.Contains(err.Error(), "invalid port") {
		t.Fatalf("expected invalid-port rejection, got %v", err)
	}
}

// compareVersions table test (was 0% covered).
func TestCompareVersionsTable(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.56.1", "0.56.1", 0},
		{"0.60.0", "0.56.1", 1},
		{"0.42.0", "0.56.1", -1},
		{"1.0", "1.0.0", 0},
		{"0.56", "0.56.1", -1},
	}
	for _, tc := range cases {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// dynamicReloadEnabledGo table test (was 0% covered).
func TestDynamicReloadEnabledGoModes(t *testing.T) {
	cases := map[string]bool{
		"": true, "always": true, "auto": true, "on": true, "1": true, "true": true,
		"never": false, "off": false, "0": false, "false": false,
	}
	for mode, want := range cases {
		t.Setenv("FPF_DYNAMIC_RELOAD", mode)
		if got := dynamicReloadEnabledGo(3); got != want {
			t.Errorf("mode %q: got %v want %v", mode, got, want)
		}
	}
	// single + one manager -> enabled; single + many -> disabled
	t.Setenv("FPF_DYNAMIC_RELOAD", "single")
	if !dynamicReloadEnabledGo(1) {
		t.Error("single/1 manager should be enabled")
	}
	if dynamicReloadEnabledGo(4) {
		t.Error("single/4 managers should be disabled")
	}
}

// buildFzfBootstrapCandidatesGo ordering and dedupe (was 0% covered).
func TestBuildFzfBootstrapCandidates(t *testing.T) {
	mockPath := createMockPath(t, "apt-cache", "apt-get", "dpkg-query", "brew")
	t.Setenv("PATH", mockPath)

	got := buildFzfBootstrapCandidatesGo([]string{"apt", "brew", "npm", "apt"})
	// npm cannot install fzf; apt deduped
	if len(got) != 2 || got[0] != "apt" || got[1] != "brew" {
		t.Fatalf("got %v, want [apt brew]", got)
	}
}
