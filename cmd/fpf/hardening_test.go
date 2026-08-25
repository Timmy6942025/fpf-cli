package main

import (
	"strings"
	"testing"
)

func TestParseDNFSearchEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantLen  int
		wantName string
	}{
		{name: "installed header", in: "Installed Packages\ndnfpkg.x86_64 1.0 repo\n", wantLen: 1, wantName: "dnfpkg"},
		{name: "available header", in: "Available Packages\navpkg.noarch 2.0 repo\n", wantLen: 1, wantName: "avpkg"},
		{name: "name-version-repo header", in: "Name Version Repo\npkg.el9 1.0 baseos\n", wantLen: 1, wantName: "pkg.el9"},
		{name: "blank lines", in: "\n\npkg.x86_64 1.0 repo\n\n", wantLen: 1, wantName: "pkg"},
		{name: "keeps dotted name without arch", in: "my.pkg.name 1.0 repo\n", wantLen: 1, wantName: "my.pkg.name"},
		{name: "single field skipped", in: "onlyname\n", wantLen: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := parseDNFSearch([]byte(tc.in))
			if len(rows) != tc.wantLen {
				t.Fatalf("len=%d want=%d rows=%+v", len(rows), tc.wantLen, rows)
			}
			if tc.wantLen > 0 && rows[0].Name != tc.wantName {
				t.Fatalf("name=%q want=%q", rows[0].Name, tc.wantName)
			}
		})
	}
}

func TestParseZypperSearchFormats(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantLen int
	}{
		// real zypper: S | Name | Type | Version | Arch | Repository
		{name: "real 6-col with pipes", in: "S | Name | Type | Version | Arch | Repository\n---+------+---------+---------+--------+-----------\ni | zypkg | package | 1.0 | x86_64 | repo\n", wantLen: 1},
		// mock format: i | x | zypkg | x | 1.0 | x | repo
		{name: "mock 7-col", in: "i | x | zypkg | x | 1.0 | x | repo\n", wantLen: 1},
		{name: "empty", in: "", wantLen: 0},
		{name: "header only", in: "S | Name | Type | Version | Arch | Repository\n", wantLen: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := parseZypperSearch([]byte(tc.in))
			if len(rows) != tc.wantLen {
				t.Fatalf("len=%d want=%d rows=%+v", len(rows), tc.wantLen, rows)
			}
			if tc.wantLen == 1 && !strings.Contains(rows[0].Desc, "version") {
				t.Fatalf("desc missing version info: %q", rows[0].Desc)
			}
		})
	}
}

func TestParsePacmanSearchIndentation(t *testing.T) {
	in := "core/pacpkg 1.0 [installed]\n    Pacman desc here\nextra/otherpkg 2.0\n    Other desc\n"
	rows := parsePacmanSearch([]byte(in))
	if len(rows) != 2 {
		t.Fatalf("len=%d rows=%+v", len(rows), rows)
	}
	if rows[0].Name != "pacpkg" || rows[0].Desc != "Pacman desc here" {
		t.Fatalf("row0=%+v", rows[0])
	}
	if rows[1].Name != "otherpkg" || rows[1].Desc != "Other desc" {
		t.Fatalf("row1=%+v", rows[1])
	}
}

func TestRankDisplayRowsScoring(t *testing.T) {
	rows := []buildDisplayRow{
		{Manager: "npm", Package: "ripgrep-cli-wrapper", Desc: "wrapper for rg"},
		{Manager: "apt", Package: "ripgrep", Desc: "regex search tool"},
		{Manager: "bun", Package: "@rg/ripgrep-js", Desc: "js bindings"},
	}
	got := rankDisplayRows("ripgrep", rows)
	if len(got) == 0 {
		t.Fatal("no ranked rows")
	}
	if got[0].Package != "ripgrep" || got[0].Manager != "apt" {
		t.Fatalf("exact match should rank first: %+v", got[0])
	}
}

func TestCapRowsForRankingDiversity(t *testing.T) {
	var rows []buildDisplayRow
	for _, m := range []string{"apt", "brew", "bun"} {
		for i := 0; i < 10; i++ {
			rows = append(rows, buildDisplayRow{Manager: m, Package: m + string(rune('a'+i))})
		}
	}
	capped := capRowsForRanking(rows, 6)
	if len(capped) != 6 {
		t.Fatalf("len=%d want 6", len(capped))
	}
	seenMgr := map[string]int{}
	for _, r := range capped {
		seenMgr[r.Manager]++
	}
	for m, c := range seenMgr {
		if c != 2 {
			t.Fatalf("manager %s has %d rows, want round-robin 2 each: %+v", m, c, seenMgr)
		}
	}
}

func TestQueryCacheKeyIncludesLimitEnv(t *testing.T) {
	t.Setenv("FPF_QUERY_RESULT_LIMIT", "")
	k1 := queryCacheKey("apt", "q", 40, 120)
	t.Setenv("FPF_QUERY_RESULT_LIMIT", "100")
	k2 := queryCacheKey("apt", "q", 40, 120)
	if k1 == k2 {
		t.Fatal("cache key should differ when FPF_QUERY_RESULT_LIMIT changes")
	}
}

func TestQueryCacheTTLClamp(t *testing.T) {
	t.Setenv("FPF_QUERY_CACHE_TTL", "999999999")
	if got := queryCacheTTLSeconds("apt"); got > 86400 {
		t.Fatalf("TTL not clamped: %d", got)
	}
	t.Setenv("FPF_QUERY_CACHE_TTL", "-50")
	if got := queryCacheTTLSeconds("apt"); got < 0 {
		t.Fatalf("negative TTL should be handled, got %d", got)
	}
}

func TestNormalizeManagerAliases(t *testing.T) {
	cases := map[string]string{
		"homebrew":         "brew",
		"Chocolatey":       "choco",
		"chocolate":        "choco",
		"portage (emerge)": "emerge",
		"portage-emerge":   "emerge",
		"portage":          "emerge",
		"win-get":          "winget",
		"BREW":             "brew",
	}
	for in, want := range cases {
		if got := normalizeManagerName(in); got != want {
			t.Errorf("normalizeManagerName(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDetectManagersRespectsNpmPolicy(t *testing.T) {
	mockPath := createMockPath(t, "apt-cache", "apt-get", "dpkg-query", "bun", "npm")
	t.Setenv("PATH", mockPath)
	t.Setenv("FPF_TEST_UNAME", "Linux")

	noQuery := detectDefaultManagersGo(false)
	hasNpmNoQuery := false
	for _, m := range noQuery {
		if m == "npm" {
			hasNpmNoQuery = true
		}
	}
	if hasNpmNoQuery {
		t.Fatal("npm should be excluded when bun present and includeNpmWithBun=false")
	}

	withQuery := detectDefaultManagersGo(true)
	hasBun := false
	hasNpm := false
	for _, m := range withQuery {
		switch m {
		case "bun":
			hasBun = true
		case "npm":
			hasNpm = true
		}
	}
	if !hasBun || !hasNpm {
		t.Fatalf("query mode should include both bun and npm: %v", withQuery)
	}
}
