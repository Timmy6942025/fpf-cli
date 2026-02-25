package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveManagersQueryAwareNpmPolicy(t *testing.T) {
	mockPath := createMockPath(t, "apt-cache", "apt-get", "dpkg-query", "bun", "npm")
	t.Setenv("PATH", mockPath)
	t.Setenv("FPF_TEST_UNAME", "Linux")

	noQuery := resolveManagers("", actionSearch, "")
	if !sliceContains(noQuery, "bun") {
		t.Fatalf("expected bun in no-query manager set, got %v", noQuery)
	}
	if sliceContains(noQuery, "npm") {
		t.Fatalf("expected npm to be excluded when bun is present for no-query search, got %v", noQuery)
	}

	withQuery := resolveManagers("", actionSearch, "ripgrep")
	if !sliceContains(withQuery, "bun") || !sliceContains(withQuery, "npm") {
		t.Fatalf("expected bun and npm in query manager set, got %v", withQuery)
	}
}

func sliceContains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func TestStderrHasTerminalGoFalseForPipe(t *testing.T) {
	oldStderr := os.Stderr
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = pipeWriter
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = pipeWriter.Close()
		_ = pipeReader.Close()
	})

	if stderrHasTerminalGo() {
		t.Fatalf("expected pipe-backed stderr to be treated as non-terminal")
	}
}

func TestRunFuzzySelectorGoCapturesStderrWhenNonTTY(t *testing.T) {
	mockPath := t.TempDir()
	writeMockExecutable(t, mockPath, "fzf", `#!/usr/bin/env bash
set -euo pipefail
printf "mock-fzf-error\n" >&2
exit 2
`)
	t.Setenv("PATH", mockPath+":/usr/bin:/bin")

	oldStderr := os.Stderr
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = pipeWriter
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = pipeWriter.Close()
		_ = pipeReader.Close()
	})

	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.tsv")
	helpFile := filepath.Join(tmpDir, "help")
	keybindFile := filepath.Join(tmpDir, "keybind")
	if err := os.WriteFile(inputFile, []byte("apt\tripgrep\tfast search tool\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := os.WriteFile(helpFile, []byte("help"), 0o644); err != nil {
		t.Fatalf("write help: %v", err)
	}
	if err := os.WriteFile(keybindFile, []byte("keys"), 0o644); err != nil {
		t.Fatalf("write keybind: %v", err)
	}

	_, err = runFuzzySelectorGo("ripgrep", inputFile, "hdr", helpFile, keybindFile, "", "", "", tmpDir)
	if err == nil {
		t.Fatalf("expected runFuzzySelectorGo to fail")
	}
	if !strings.Contains(err.Error(), "mock-fzf-error") {
		t.Fatalf("expected error to include stderr output, got %q", err.Error())
	}
}

func TestDynamicReloadManagersDefaultAndOverride(t *testing.T) {
	allManagers := []string{"apt", "flatpak", "bun", "npm"}

	gotDefault := dynamicReloadManagers(allManagers)
	if strings.Join(gotDefault, ",") != "apt,bun" {
		t.Fatalf("dynamicReloadManagers default=%v want=[apt bun]", gotDefault)
	}

	t.Setenv("FPF_DYNAMIC_RELOAD_MANAGERS", "all")
	gotAll := dynamicReloadManagers(allManagers)
	if strings.Join(gotAll, ",") != "apt,flatpak,bun,npm" {
		t.Fatalf("dynamicReloadManagers all=%v want all managers", gotAll)
	}

	t.Setenv("FPF_DYNAMIC_RELOAD_MANAGERS", "npm,apt,missing")
	gotSubset := dynamicReloadManagers(allManagers)
	if strings.Join(gotSubset, ",") != "npm,apt" {
		t.Fatalf("dynamicReloadManagers subset=%v want=[npm apt]", gotSubset)
	}

	t.Setenv("FPF_DYNAMIC_RELOAD_MANAGERS", "missing,unknown")
	gotInvalid := dynamicReloadManagers(allManagers)
	if strings.Join(gotInvalid, ",") != "apt,bun" {
		t.Fatalf("dynamicReloadManagers invalid override=%v want default fast subset [apt bun]", gotInvalid)
	}
}
