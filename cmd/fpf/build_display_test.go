package main

import (
	"testing"
)

func TestManagerSearchConfig(t *testing.T) {
	t.Setenv("FPF_NO_QUERY_NPM_LIMIT", "120")
	t.Setenv("FPF_NO_QUERY_RESULT_LIMIT", "120")
	t.Setenv("FPF_QUERY_PER_MANAGER_LIMIT", "40")

	tests := []struct {
		name         string
		manager      string
		query        string
		wantQuery    string
		wantLimit    int
		wantNpmLimit int
	}{
		{name: "apt no query defaults", manager: "apt", query: "", wantQuery: "a", wantLimit: 120, wantNpmLimit: 120},
		{name: "brew no query defaults", manager: "brew", query: "", wantQuery: "aa", wantLimit: 120, wantNpmLimit: 120},
		{name: "explicit query uses per manager limit", manager: "flatpak", query: "ripgrep", wantQuery: "ripgrep", wantLimit: 40, wantNpmLimit: 120},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotQuery, gotLimit, gotNpmLimit := managerSearchConfig(tc.manager, tc.query)
			if gotQuery != tc.wantQuery {
				t.Fatalf("managerSearchConfig query=%q want=%q", gotQuery, tc.wantQuery)
			}
			if gotLimit != tc.wantLimit {
				t.Fatalf("managerSearchConfig limit=%d want=%d", gotLimit, tc.wantLimit)
			}
			if gotNpmLimit != tc.wantNpmLimit {
				t.Fatalf("managerSearchConfig npm limit=%d want=%d", gotNpmLimit, tc.wantNpmLimit)
			}
		})
	}
}

func TestMergeDisplayRows(t *testing.T) {
	rows := []buildDisplayRow{
		{Manager: "apt", Package: "ripgrep", Desc: "desc-a"},
		{Manager: "apt", Package: "ripgrep", Desc: "desc-b"},
		{Manager: "bun", Package: "ripgrep", Desc: "desc-c"},
	}

	merged := mergeDisplayRows(rows)
	if len(merged) != 2 {
		t.Fatalf("mergeDisplayRows len=%d want=2", len(merged))
	}
	if merged[0].Manager != "apt" || merged[0].Package != "ripgrep" {
		t.Fatalf("unexpected first row: %+v", merged[0])
	}
	if merged[1].Manager != "bun" || merged[1].Package != "ripgrep" {
		t.Fatalf("unexpected second row: %+v", merged[1])
	}
}

func TestProcessDisplayRowsAppliesRankingAndLimit(t *testing.T) {
	t.Setenv("FPF_SKIP_INSTALLED_MARKERS", "1")
	t.Setenv("FPF_QUERY_RESULT_LIMIT", "2")

	rows := []buildDisplayRow{
		{Manager: "apt", Package: "z-ripgrep", Desc: "late"},
		{Manager: "apt", Package: "ripgrep", Desc: "target"},
		{Manager: "apt", Package: "ripgrep", Desc: "duplicate"},
		{Manager: "bun", Package: "rg", Desc: "alias"},
	}

	got := processDisplayRows("ripgrep", []string{"apt", "bun"}, rows)
	if len(got) != 2 {
		t.Fatalf("processDisplayRows len=%d want=2", len(got))
	}
	if got[0].Package != "ripgrep" {
		t.Fatalf("expected exact match first, got %+v", got[0])
	}
}

func TestRenderBuildDisplayRowsTSVContract(t *testing.T) {
	rows := []buildDisplayRow{
		{Manager: "apt", Package: "ripgrep", Desc: "* installed"},
		{Manager: "bun", Package: "rg", Desc: "  alias"},
	}

	rendered := renderBuildDisplayRows(rows)
	parsed := parseDisplayRows([]byte(rendered))
	if len(parsed) != 2 {
		t.Fatalf("parseDisplayRows len=%d want=2", len(parsed))
	}
	if parsed[0].Manager != "apt" || parsed[0].Package != "ripgrep" {
		t.Fatalf("unexpected first parsed row: %+v", parsed[0])
	}
	if parsed[1].Manager != "bun" || parsed[1].Package != "rg" {
		t.Fatalf("unexpected second parsed row: %+v", parsed[1])
	}
}
