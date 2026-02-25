package main

import (
	"testing"
	"time"
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

func TestSkipNoQueryInstalledMarkers(t *testing.T) {
	if !skipNoQueryInstalledMarkers("", []string{"apt", "bun"}) {
		t.Fatal("expected no-query multi-manager to skip installed marker lookups")
	}
	if skipNoQueryInstalledMarkers("ripgrep", []string{"apt", "bun"}) {
		t.Fatal("expected non-empty query to keep installed marker lookups enabled")
	}
	if skipNoQueryInstalledMarkers("", []string{"apt"}) {
		t.Fatal("expected single-manager no-query to keep installed marker lookups enabled")
	}
	t.Setenv("FPF_NO_QUERY_INCLUDE_INSTALLED_MARKERS", "1")
	if skipNoQueryInstalledMarkers("", []string{"apt", "bun"}) {
		t.Fatal("expected explicit opt-in to keep installed marker lookups enabled")
	}
}

func TestRankCandidateLimitAndCapRowsForRanking(t *testing.T) {
	t.Setenv("FPF_QUERY_RESULT_LIMIT", "50")
	limit := rankCandidateLimit("ripgrep")
	if limit != 200 {
		t.Fatalf("rankCandidateLimit=%d want=200", limit)
	}

	rows := []buildDisplayRow{
		{Manager: "apt", Package: "a1"},
		{Manager: "apt", Package: "a2"},
		{Manager: "bun", Package: "b1"},
		{Manager: "bun", Package: "b2"},
		{Manager: "flatpak", Package: "f1"},
		{Manager: "flatpak", Package: "f2"},
	}
	capped := capRowsForRanking(rows, 4)
	if len(capped) != 4 {
		t.Fatalf("capRowsForRanking len=%d want=4", len(capped))
	}
	// round-robin should preserve manager diversity in the front set.
	if capped[0].Manager != "apt" || capped[1].Manager != "bun" || capped[2].Manager != "flatpak" {
		t.Fatalf("unexpected round-robin ordering: %+v", capped)
	}
}

func TestAllowBunNpmFallback(t *testing.T) {
	if !allowBunNpmFallback("bun", 1, false) {
		t.Fatal("expected single-manager bun to allow npm fallback")
	}
	if allowBunNpmFallback("bun", 3, true) {
		t.Fatal("expected bun fallback disabled when npm is already in multi-manager set")
	}
	if allowBunNpmFallback("bun", 3, false) {
		t.Fatal("expected bun fallback disabled in multi-manager mode by default")
	}
	t.Setenv("FPF_BUN_ALLOW_NPM_FALLBACK_MULTI", "1")
	if !allowBunNpmFallback("bun", 3, false) {
		t.Fatal("expected env override to enable bun fallback in multi-manager mode")
	}
	if !allowBunNpmFallback("apt", 3, true) {
		t.Fatal("expected non-bun manager fallback setting to remain enabled")
	}
}

func TestMultiManagerSearchTimeout(t *testing.T) {
	if got := multiManagerSearchTimeout("bun", "ripgrep", 1); got != 0 {
		t.Fatalf("single-manager timeout=%s want=0", got)
	}
	if got := multiManagerSearchTimeout("bun", "ripgrep", 3); got != 1200*time.Millisecond {
		t.Fatalf("bun query timeout=%s want=1200ms", got)
	}
	if got := multiManagerSearchTimeout("flatpak", "", 3); got != 700*time.Millisecond {
		t.Fatalf("flatpak no-query timeout=%s want=700ms", got)
	}
	t.Setenv("FPF_SEARCH_TIMEOUT_BUN_MS", "250")
	if got := multiManagerSearchTimeout("bun", "ripgrep", 3); got != 250*time.Millisecond {
		t.Fatalf("bun env override timeout=%s want=250ms", got)
	}
	t.Setenv("FPF_SEARCH_TIMEOUT_BUN_MS", "")
	t.Setenv("FPF_MULTI_MANAGER_SEARCH_TIMEOUT_MS", "400")
	if got := multiManagerSearchTimeout("flatpak", "ripgrep", 3); got != 400*time.Millisecond {
		t.Fatalf("global env override timeout=%s want=400ms", got)
	}
}
