package main

import (
	"strings"
	"testing"
)

func TestHasMissingManagerValue(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short flag missing value", args: []string{"-m"}, want: true},
		{name: "short flag next option", args: []string{"-m", "-U"}, want: true},
		{name: "long flag missing value", args: []string{"--manager"}, want: true},
		{name: "long flag next option", args: []string{"--manager", "--refresh"}, want: true},
		{name: "equals empty", args: []string{"--manager="}, want: true},
		{name: "equals whitespace", args: []string{"--manager=   "}, want: true},
		{name: "short valid", args: []string{"-m", "brew", "--refresh"}, want: false},
		{name: "long valid", args: []string{"--manager", "brew", "--refresh"}, want: false},
		{name: "equals valid", args: []string{"--manager=brew", "--refresh"}, want: false},
		{name: "after terminator ignored", args: []string{"--", "--manager"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasMissingManagerValue(tc.args); got != tc.want {
				t.Fatalf("hasMissingManagerValue(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestNormalizeManagerName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "homebrew", want: "brew"},
		{in: "Chocolatey", want: "choco"},
		{in: "  portage (emerge)  ", want: "emerge"},
		{in: "win-get", want: "winget"},
		{in: "brew", want: "brew"},
	}

	for _, tc := range tests {
		if got := normalizeManagerName(tc.in); got != tc.want {
			t.Fatalf("normalizeManagerName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeManagerArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "long manager value normalized",
			in:   []string{"--manager", "Chocolatey", "--refresh"},
			want: []string{"--manager", "choco", "--refresh"},
		},
		{
			name: "short manager value normalized",
			in:   []string{"-m", "homebrew", "-U"},
			want: []string{"-m", "brew", "-U"},
		},
		{
			name: "equals manager normalized",
			in:   []string{"--manager=win-get", "--refresh"},
			want: []string{"--manager=winget", "--refresh"},
		},
		{
			name: "args after separator unchanged",
			in:   []string{"--", "--manager", "Chocolatey"},
			want: []string{"--", "--manager", "Chocolatey"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeManagerArgs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("normalizeManagerArgs(%v) length = %d, want %d", tc.in, len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("normalizeManagerArgs(%v)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseManagerActionInput(t *testing.T) {
	input, ok, err := parseManagerActionInput([]string{"--go-manager-action", "refresh", "--go-manager", "Chocolatey", "--", "pkg1", "pkg2"})
	if err != nil {
		t.Fatalf("parseManagerActionInput returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected hidden manager action mode")
	}
	if input.Action != "refresh" {
		t.Fatalf("action = %q, want refresh", input.Action)
	}
	if input.Manager != "choco" {
		t.Fatalf("manager = %q, want choco", input.Manager)
	}
	if len(input.Packages) != 2 || input.Packages[0] != "pkg1" || input.Packages[1] != "pkg2" {
		t.Fatalf("packages = %v, want [pkg1 pkg2]", input.Packages)
	}

	_, ok, err = parseManagerActionInput([]string{"--go-manager", "brew"})
	if err == nil || !ok {
		t.Fatalf("expected error for missing action, got ok=%v err=%v", ok, err)
	}
}

func TestParseSearchInput(t *testing.T) {
	input, ok, err := parseSearchInput([]string{"--go-search-entries", "--go-manager", "homebrew", "--go-query", "ripgrep", "--go-limit", "40", "--go-npm-search-limit", "120"})
	if err != nil {
		t.Fatalf("parseSearchInput returned error: %v", err)
	}
	if !ok {
		t.Fatal("expected search mode")
	}
	if input.Manager != "brew" || input.Query != "ripgrep" || input.Limit != 40 || input.NPMSearchLimit != 120 {
		t.Fatalf("unexpected parsed input: %+v", input)
	}
}

func TestParseSearchInputIgnoresManagerActionFlags(t *testing.T) {
	_, ok, err := parseSearchInput([]string{"--go-manager-action", "install", "--go-manager", "apt", "--", "ripgrep"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("search mode should not activate for manager action flags")
	}
}

func TestParsePreviewRequest(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantEnabled bool
		wantManager string
		wantPkg     string
	}{
		{
			name:        "preview with manager and package",
			args:        []string{"--preview-item", "--manager", "Chocolatey", "--", "ripgrep"},
			wantEnabled: true,
			wantManager: "choco",
			wantPkg:     "ripgrep",
		},
		{
			name:        "preview missing package",
			args:        []string{"--preview-item", "--manager", "brew"},
			wantEnabled: true,
			wantManager: "brew",
			wantPkg:     "",
		},
		{
			name:        "no preview mode",
			args:        []string{"--manager", "brew", "--", "ripgrep"},
			wantEnabled: false,
			wantManager: "brew",
			wantPkg:     "ripgrep",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			enabled, manager, pkg := parsePreviewRequest(tc.args)
			if enabled != tc.wantEnabled {
				t.Fatalf("enabled = %v, want %v", enabled, tc.wantEnabled)
			}
			if manager != tc.wantManager {
				t.Fatalf("manager = %q, want %q", manager, tc.wantManager)
			}
			if pkg != tc.wantPkg {
				t.Fatalf("pkg = %q, want %q", pkg, tc.wantPkg)
			}
		})
	}
}

func TestParseNpmInstalled(t *testing.T) {
	raw := strings.Join([]string{
		"/usr/lib/node_modules",
		"/usr/lib/node_modules/npmpkg",
		"/usr/lib/node_modules/@opencode-ai/cli",
	}, "\n")

	got := parseNpmInstalled([]byte(raw))
	if len(got) != 2 {
		t.Fatalf("parseNpmInstalled len = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != "npmpkg" {
		t.Fatalf("parseNpmInstalled[0] = %q, want npmpkg", got[0])
	}
	if got[1] != "@opencode-ai/cli" {
		t.Fatalf("parseNpmInstalled[1] = %q, want @opencode-ai/cli", got[1])
	}
}

func TestParseBunInstalled(t *testing.T) {
	raw := strings.Join([]string{
		"/Users/test/.bun/install/global node_modules (2)",
		"+- opencode-ai@1.2.6",
		"+- @openai/codex@0.101.0",
	}, "\n")

	got := parseBunInstalled([]byte(raw))
	if len(got) != 2 {
		t.Fatalf("parseBunInstalled len = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != "opencode-ai" {
		t.Fatalf("parseBunInstalled[0] = %q, want opencode-ai", got[0])
	}
	if got[1] != "@openai/codex" {
		t.Fatalf("parseBunInstalled[1] = %q, want @openai/codex", got[1])
	}
}

func TestParseDynamicReloadRequest(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantOn  bool
		wantQry string
	}{
		{name: "dynamic reload with query", args: []string{"--dynamic-reload", "--", "ripgrep"}, wantOn: true, wantQry: "ripgrep"},
		{name: "dynamic reload missing query", args: []string{"--dynamic-reload"}, wantOn: true, wantQry: ""},
		{name: "non dynamic args", args: []string{"--feed-search", "--", "ripgrep"}, wantOn: false, wantQry: "ripgrep"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			on, query := parseDynamicReloadRequest(tc.args)
			if on != tc.wantOn {
				t.Fatalf("on=%v want=%v", on, tc.wantOn)
			}
			if query != tc.wantQry {
				t.Fatalf("query=%q want=%q", query, tc.wantQry)
			}
		})
	}
}

func TestParseIPCRequest(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		args    []string
		wantOn  bool
		wantQry string
	}{
		{name: "ipc reload with query", flag: "--ipc-reload", args: []string{"--ipc-reload", "--", "ripgrep"}, wantOn: true, wantQry: "ripgrep"},
		{name: "ipc notify with query", flag: "--ipc-query-notify", args: []string{"--ipc-query-notify", "--", "fd"}, wantOn: true, wantQry: "fd"},
		{name: "different flag ignored", flag: "--ipc-reload", args: []string{"--ipc-query-notify", "--", "fd"}, wantOn: false, wantQry: "fd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			on, query := parseIPCRequest(tc.args, tc.flag)
			if on != tc.wantOn {
				t.Fatalf("on=%v want=%v", on, tc.wantOn)
			}
			if query != tc.wantQry {
				t.Fatalf("query=%q want=%q", query, tc.wantQry)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote(""); got != "''" {
		t.Fatalf("shellQuote empty = %q", got)
	}
	if got := shellQuote("plain"); got != "'plain'" {
		t.Fatalf("shellQuote plain = %q", got)
	}
	if got := shellQuote("a'b"); got != "'a'\\''b'" {
		t.Fatalf("shellQuote quote = %q", got)
	}
}
