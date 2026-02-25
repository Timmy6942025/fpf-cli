package flatpak

import "testing"

func TestCacheFilterUsesAppIDAsResultName(t *testing.T) {
	cache := &Cache{Apps: []App{{
		ID:      "org.gnome.Calculator",
		Name:    "Calculator",
		Summary: "Perform arithmetic",
	}}}

	rows := cache.Filter("calculator")
	if len(rows) != 1 {
		t.Fatalf("Filter returned %d rows, want 1", len(rows))
	}
	if rows[0].Name != "org.gnome.Calculator" {
		t.Fatalf("row name = %q, want flatpak app id", rows[0].Name)
	}
}

func TestCacheFilterFallsBackToDisplayNameWithoutID(t *testing.T) {
	cache := &Cache{Apps: []App{{
		ID:      "",
		Name:    "Only Name App",
		Summary: "Fallback path",
	}}}

	rows := cache.Filter("")
	if len(rows) != 1 {
		t.Fatalf("Filter returned %d rows, want 1", len(rows))
	}
	if rows[0].Name != "Only Name App" {
		t.Fatalf("row name = %q, want fallback app name", rows[0].Name)
	}
}
