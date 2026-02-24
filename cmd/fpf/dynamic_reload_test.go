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

func TestResolveFeedSearchInvocation(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		managerArg  string
		wantArgs    []string
		wantMgrList string
	}{
		{
			name:        "single manager uses --manager",
			query:       "ripgrep",
			managerArg:  "apt",
			wantArgs:    []string{"--manager", "apt", "--feed-search", "--", "ripgrep"},
			wantMgrList: "",
		},
		{
			name:        "multi manager uses override env",
			query:       "ripgrep",
			managerArg:  "apt,bun,flatpak",
			wantArgs:    []string{"--feed-search", "--", "ripgrep"},
			wantMgrList: "apt,bun,flatpak",
		},
		{
			name:        "empty manager uses auto",
			query:       "ripgrep",
			managerArg:  "",
			wantArgs:    []string{"--feed-search", "--", "ripgrep"},
			wantMgrList: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotArgs, gotMgrList := resolveFeedSearchInvocation(tc.query, tc.managerArg)
			if len(gotArgs) != len(tc.wantArgs) {
				t.Fatalf("resolveFeedSearchInvocation args len=%d want=%d (%v)", len(gotArgs), len(tc.wantArgs), gotArgs)
			}
			for i := range gotArgs {
				if gotArgs[i] != tc.wantArgs[i] {
					t.Fatalf("resolveFeedSearchInvocation arg[%d]=%q want=%q", i, gotArgs[i], tc.wantArgs[i])
				}
			}
			if gotMgrList != tc.wantMgrList {
				t.Fatalf("resolveFeedSearchInvocation manager list=%q want=%q", gotMgrList, tc.wantMgrList)
			}
		})
	}
}
