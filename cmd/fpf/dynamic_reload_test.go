package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitManagerArg(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "single", in: "apt", want: []string{"apt"}},
		{name: "csv", in: "apt,bun,flatpak", want: []string{"apt", "bun", "flatpak"}},
		{name: "trim and dedupe", in: " apt , bun,apt , flatpak ", want: []string{"apt", "bun", "flatpak"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitManagerArg(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("splitManagerArg(%q) len=%d want=%d (%v)", tc.in, len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("splitManagerArg(%q)[%d]=%q want=%q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestResolveReloadManagerArg(t *testing.T) {
	mockPath := createMockPath(t, "apt-cache", "apt-get", "dpkg-query", "bun", "flatpak", "npm")
	t.Setenv("PATH", mockPath)

	t.Run("override uses exact manager", func(t *testing.T) {
		t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "bun")
		t.Setenv("FPF_IPC_MANAGER_LIST", "apt,bun,flatpak")

		got, ok := resolveReloadManagerArg()
		if !ok {
			t.Fatal("expected override manager to resolve")
		}
		if got != "bun" {
			t.Fatalf("resolveReloadManagerArg override=%q want=bun", got)
		}
	})

	t.Run("csv preserves order and dedupes", func(t *testing.T) {
		t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "")
		t.Setenv("FPF_IPC_MANAGER_LIST", "apt,bun,apt,flatpak,bun")

		got, ok := resolveReloadManagerArg()
		if !ok {
			t.Fatal("expected manager csv to resolve")
		}
		if got != "apt,bun,flatpak" {
			t.Fatalf("resolveReloadManagerArg csv=%q want=apt,bun,flatpak", got)
		}
	})

	t.Run("rejects unsupported manager", func(t *testing.T) {
		t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "")
		t.Setenv("FPF_IPC_MANAGER_LIST", "apt,unknown")

		if got, ok := resolveReloadManagerArg(); ok {
			t.Fatalf("expected unsupported manager to fail, got ok with %q", got)
		}
	})

	t.Run("rejects unready manager", func(t *testing.T) {
		t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "")
		t.Setenv("FPF_IPC_MANAGER_LIST", "apt,brew")

		if got, ok := resolveReloadManagerArg(); ok {
			t.Fatalf("expected unready manager to fail, got ok with %q", got)
		}
	})

	t.Run("no override and no list does not fall back to autodetect", func(t *testing.T) {
		t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "")
		t.Setenv("FPF_IPC_MANAGER_LIST", "")

		if got, ok := resolveReloadManagerArg(); ok {
			t.Fatalf("expected missing reload manager inputs to fail, got %q", got)
		}
	})
}

func TestReloadManagerSetParity(t *testing.T) {
	mockPath := createMockPath(t, "apt-cache", "apt-get", "dpkg-query", "bun", "flatpak", "npm")
	t.Setenv("PATH", mockPath)
	t.Setenv("FPF_TEST_UNAME", "Linux")

	initialManagers := resolveManagers("", actionSearch)
	if len(initialManagers) == 0 {
		t.Fatal("expected at least one auto manager for parity test")
	}
	initialCSV := strings.Join(initialManagers, ",")

	t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "")
	t.Setenv("FPF_IPC_MANAGER_LIST", initialCSV)
	reloadCSV, ok := resolveReloadManagerArg()
	if !ok {
		t.Fatal("expected reload manager list to resolve from session csv")
	}
	if reloadCSV != initialCSV {
		t.Fatalf("reload managers=%q want initial=%q", reloadCSV, initialCSV)
	}

	t.Setenv("FPF_IPC_MANAGER_OVERRIDE", "bun")
	t.Setenv("FPF_IPC_MANAGER_LIST", initialCSV)
	reloadOverride, ok := resolveReloadManagerArg()
	if !ok {
		t.Fatal("expected reload override manager to resolve")
	}
	if reloadOverride != "bun" {
		t.Fatalf("reload override=%q want bun", reloadOverride)
	}
	if strings.Contains(reloadOverride, "npm") {
		t.Fatalf("bun override should not expand to npm: %q", reloadOverride)
	}
}

func createMockPath(t *testing.T, names ...string) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("failed to write mock binary %s: %v", name, err)
		}
	}
	return dir
}
