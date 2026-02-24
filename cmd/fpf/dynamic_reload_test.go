package main

import "testing"

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

func TestMergeFeedOutputs(t *testing.T) {
	out := mergeFeedOutputs([][]byte{
		[]byte("apt\tripgrep\tGNU grep alt\napt\tfd\tFast find\n"),
		[]byte("bun\tripgrep\tJS wrapper\napt\tripgrep\tGNU grep alt\n"),
		[]byte("flatpak\torg.gimp.GIMP\tImage editor\n"),
	})

	got := string(out)
	want := "apt\tripgrep\tGNU grep alt\napt\tfd\tFast find\nbun\tripgrep\tJS wrapper\nflatpak\torg.gimp.GIMP\tImage editor\n"
	if got != want {
		t.Fatalf("mergeFeedOutputs mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}
