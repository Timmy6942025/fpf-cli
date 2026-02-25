package main

import (
	"fmt"
	"testing"
)

func BenchmarkDynamicReloadPipeline(b *testing.B) {
	b.ReportAllocs()
	b.Setenv("FPF_SKIP_INSTALLED_MARKERS", "1")
	b.Setenv("FPF_QUERY_RESULT_LIMIT", "200")

	managers := []string{"apt", "bun", "flatpak"}
	baseRows := make([]buildDisplayRow, 0, 3000)
	for i := 0; i < 1000; i++ {
		baseRows = append(baseRows,
			buildDisplayRow{Manager: "apt", Package: fmt.Sprintf("apt-pkg-%04d", i), Desc: "apt package"},
			buildDisplayRow{Manager: "bun", Package: fmt.Sprintf("bun-pkg-%04d", i), Desc: "bun package"},
			buildDisplayRow{Manager: "flatpak", Package: fmt.Sprintf("org.example.App%04d", i), Desc: "flatpak app"},
		)
	}
	baseRows = append(baseRows, buildDisplayRow{Manager: "apt", Package: "ripgrep", Desc: "fast search"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows := append([]buildDisplayRow(nil), baseRows...)
		_ = processDisplayRows("ripgrep", managers, rows)
	}
}
