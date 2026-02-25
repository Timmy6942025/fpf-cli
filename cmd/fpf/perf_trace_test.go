package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPerfTrace(t *testing.T) {
	mockPath := t.TempDir()
	writeMockExecutable(t, mockPath, "bun", `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "search" ]]; then
    printf "name desc\n"
    printf "ripgrep fast search tool\n"
fi
`)
	writeMockExecutable(t, mockPath, "fzf", `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "--help" ]]; then
    printf "usage: fzf [options]\n"
    exit 0
fi
IFS= read -r line || true
if [[ -n "${line}" ]]; then
    printf "%s\n" "${line}"
fi
`)

	t.Setenv("PATH", mockPath+":/usr/bin:/bin")
	t.Setenv("FPF_ENABLE_QUERY_CACHE", "0")

	var buf bytes.Buffer
	perfTraceMu.Lock()
	prev := perfTraceWriter
	perfTraceWriter = &buf
	perfTraceMu.Unlock()
	t.Cleanup(func() {
		perfTraceMu.Lock()
		perfTraceWriter = prev
		perfTraceMu.Unlock()
	})

	_ = resolveManagers("bun", actionSearch)
	_ = collectRowsForManager("bun", "ripgrep")
	_ = processDisplayRows("ripgrep", []string{"bun"}, []buildDisplayRow{{Manager: "bun", Package: "ripgrep", Desc: "fast search tool"}})

	tmpDir := t.TempDir()
	inputFile := filepath.Join(tmpDir, "input.tsv")
	helpFile := filepath.Join(tmpDir, "help")
	keybindFile := filepath.Join(tmpDir, "keybinds")
	if err := os.WriteFile(inputFile, []byte("bun\tripgrep\tfast search tool\n"), 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}
	if err := os.WriteFile(helpFile, []byte("help"), 0o644); err != nil {
		t.Fatalf("write help: %v", err)
	}
	if err := os.WriteFile(keybindFile, []byte("keys"), 0o644); err != nil {
		t.Fatalf("write keybinds: %v", err)
	}
	if _, err := runFuzzySelectorGo("ripgrep", inputFile, "hdr", helpFile, keybindFile, "", "", tmpDir); err != nil {
		t.Fatalf("runFuzzySelectorGo: %v", err)
	}

	trace := buf.String()
	if perfTraceEnabled() {
		t.Log(trace)
		for _, expected := range []string{"stage=manager-resolve", "stage=search", "stage=rank", "stage=fzf"} {
			if !strings.Contains(trace, expected) {
				t.Fatalf("expected perf trace to contain %q, got:\n%s", expected, trace)
			}
		}
		return
	}

	logPerfTraceStage("search", time.Now())
	if got := strings.TrimSpace(buf.String()); got != "" {
		t.Fatalf("expected no trace output when disabled, got %q", got)
	}
}

func writeMockExecutable(t *testing.T, dir, name, script string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write mock executable %s: %v", name, err)
	}
}
