# fpf-cli Architecture

This document describes the show-car restoration of fpf-cli — from a 20-year abandoned garage to a clean, maintainable codebase.

## Overview

```
fpf (bash wrapper)
  └─> bin/fpf-go-* (Go binary, cmd/fpf)
       ├─> CLI layer (parse, confirm, dispatch)
       ├─> Search layer (managers → rows)
       ├─> Display layer (merge → rank → cache → TSV)
       └─> FZF layer (preview, live reload, IPC)
```

## Package Layout

```
cmd/fpf/
  main.go               # Entry, arg dispatch (330 lines)
  manager_actions.go    # executeManagerAction for 13 managers (407 lines)
  cli_runtime.go        # runCLI, parseCLIInput, collect*DisplayRows (526 lines)
  cli_fzf.go            # fzf bootstrap, version checks, runFuzzySelectorGo (870 lines)
  cli_managers.go       # Manager detection, labels, flathub checks (304 lines)
  build_display.go      # buildDisplayRows, collectManagerRows, processDisplayRows (471 lines)
  display_cache.go      # Query + installed cache, TTL, fingerprint (527 lines)
  display_merge.go      # mergeDisplayRows (132 lines)
  display_rank.go       # rankDisplayRows, scoreRows (313 lines)
  search_entries.go     # executeSearchEntries, timeouts (411 lines)
  search_parsers.go     # 13x parse*Search + dedupe (387 lines)
  search_catalog.go     # Apt/Brew catalog caching (272 lines)
  dynamic_reload.go     # Live reload handler (298 lines)
  ipc_actions.go        # IPC query-notify/reload (201 lines)
  installed_entries.go  # parse*Installed (471 lines)
  logger.go             # Structured debug/error logging (40 lines)
  perf_trace.go         # Timing

internal/flatpak/
  cache.go    # AppStream cache with RWMutex + 60s timeout
  parser.go   # XML + gzip handling (fixed Seek)
  types.go    # App/Cache types

tests/
  smoke.sh              # Legacy 3018-line bash integration (mock 20+ binaries)
  fixtures/             # Minimal fixtures (14 files)
  integration_test.go   # New Go-native smoke (5 tests, replaces bash over time)
```

**Before:** 4 god files >700 LOC (1674, 1044, 959, 723) in single `package main`, 0 `internal` except flatpak.
**After:** Max 870 LOC/file, clear separation, no logic change — same package, just file moves, so `go vet` and `go test` still pass.

## Data Flow

1. **CLI** `parseCLIInput` → `resolveManagers` (detects `apt`/`brew`/etc. via `exec.LookPath`, respects `--manager`, `FPF_TEST_UNAME`)
2. **Search** `collectManagerRows` fans out via `sync.WaitGroup` + `chan managerRows` (parallel per-manager, `multiManagerSearchTimeout` caps `bun`/`npm`/`flatpak` at 10s, others 0)
3. **Catalog** `loadAptCatalogRows` (`apt-cache dumpavail` → TSV cache) / `loadBrewCatalogRows` (`brew formulae` + `casks` → TSV) — cached via `cacheRootPath()` (`$XDG_CACHE_HOME/fpf` or `~/.cache/fpf` or `/tmp/fpf-cache` with `0o700`)
4. **Display** `processDisplayRows`: `mergeDisplayRows` (sort + dedupe) → `applyInstalledMarkers` (parallel `loadInstalledSet` via cache, `* ` vs `  `) → `rankDisplayRows` (score 0-12, manager bias) → `applyQueryLimit`
5. **FZF** `runFuzzySelectorGo` builds `previewCmd` (`FPF_SESSION_TMP_ROOT=... bash -c 'exec ... "$1" "$2"' -- {1} {2}`) and `change:reload` (`FPF_... bash -c 'exec "$0" --dynamic-reload -- "$1"' -- "{q}"` — double-quoted `{q}` + Go joins `args[i+1:]` for multi-word safety) plus `ctrl-r` full reload and `result:change-prompt` reset.

## Caching

- **Query cache** `go-query/<manager>/<hash>.tsv` + `.meta` (TTL `apt 180`, `brew 120`, `pacman 180`, `bun 300`, override via `FPF_*_QUERY_CACHE_TTL`), fingerprint `3|manager|binpath|q=...|limit=...` includes `exec.LookPath` so binary updates invalidate.
- **Installed cache** `go-installed/<manager>.txt` + `.meta` (TTL 300, `FPF_INSTALLED_CACHE_TTL`)
- **Catalog cache** `search-catalog/apt/<hash>.tsv` (fingerprint includes `apt-dumpavail.txt` mtime/size for fixtures)
- All `MkdirAll` now `0o700` (was `0o755` world-readable) and `CreateTemp` + `Rename` atomic; write errors now checked and cleaned up.

## FZF Bootstrap

`ensureFzfGo` → `fzfCommandAvailableGo` (checks `FPF_TEST_MOCK_BIN` + `/tmp` heuristic to prefer mock over `~/.local/share/fpf/fzf/fzf`) → `installFzfWithManagerGo` (tries `apt`/`brew`/etc.) → `installFzfFromReleaseFallbackGo` (downloads `junegunn/fzf` release, verifies SHA256 via `fetchFzfChecksum`, extracts `tar.gz`/`zip` with `0o755`, atomic rename). Version gated to `>=0.56.1` for `change:reload` + `result` bind.

## Flatpak

`internal/flatpak/cache.go` uses `sync.RWMutex` + `globalCacheLoaded bool` (was `sync.Once` race) and `context.WithTimeout(60s)` for `flatpak update --appstream`. `parser.go` fixed `Seek(0)` before gzip check so uncompressed `appstream.xml` not truncated.

## CI

`test.yml` (was `main` only, Node 20, no cache) → triggers on `main`+`master`, `setup-go` with `cache:true`, Node 22, `gofmt -l` + `go vet` gates, `benchmark` non-blocking.
`publish-npm.yml` (was `master` only, Node 20, no Go) → `main`+`master`, `id-token: write`, `concurrency`, Node 22, plus `Verify build` step (`go vet`+`go test`) and parallel `go build` + `SHA256SUMS` generation.

## Build

`scripts/build-go-binaries.sh` now `go vet` first, derives `VERSION` from `package.json`, `LDFLAGS="-X main.version=..."`, parallel `& wait`, and `sha256sum`/`shasum` checksums. `fpf` wrapper deduplicated OS detection, added fallback warning and `arch` handling, `FPF_PACKAGE_JSON` export.

## Future

- Finish `internal/` extraction: move `search/`, `display/`, `fzf/`, `managers/` out of `cmd/fpf` into proper `internal` packages (currently just file splits, same `package main` for safety).
- Replace `tests/smoke.sh` entirely with `go test -run TestIntegration` + `testscript` PTY tests.
- Add `staticcheck` and `golangci-lint` to CI.
