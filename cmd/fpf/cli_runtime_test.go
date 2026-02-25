package main

import "testing"

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
