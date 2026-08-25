# Contributing to fpf-cli

Thanks for helping restore this show car.

## Quick Start

```bash
go vet ./...          # must pass
gofmt -l .            # must be empty (we use gofmt, not gofumpt)
go test ./...         # unit + integration
npm test              # full smoke (3018-line bash, 60+ scenarios with mock 20+ binaries)
bash scripts/build-go-binaries.sh  # parallel build + SHA256SUMS, embeds version via -X
./fpf --version       # from wrapper
./bin/fpf-go-linux-amd64 --feed-search -- ripgrep | head
```

Requirements: `go 1.24+`, `bash`, `fzf 0.56.1+` (auto-bootstrapped), `node 22` for `npm pack`.

## Architecture

See `ARCHITECTURE.md` for package layout and data flow. Key rule: **no god files** — max ~500 LOC/file. If you touch `cli_runtime.go`, split it.

## Adding a Manager

1. Add to `isManagerSupported` in `cmd/fpf/dynamic_reload.go` and `cli_managers.go`
2. Add `binaries` in `isManagerCommandReady`
3. Add `parse*Search` in `cmd/fpf/search_parsers.go` and `parse*Installed` in `cmd/fpf/installed_entries.go`
4. Add `executeSearchEntries` + `executeInstalledEntries` cases
5. Add `executeManagerAction` case in `cmd/fpf/manager_actions.go` (install/remove/show_info/update/refresh)
6. Add label in `managerLabelGo`
7. Add mock in `tests/smoke.sh` `mockcmd` case and fixture if needed
8. Add `run_search_install_test` etc. in smoke and a `TestIntegration*` case in `cmd/fpf/integration_test.go`

## Env Vars

All `FPF_*` are documented in `README.md#Notes`. When adding a new one, update README and add `parseEnvInt`/`parseEnvFloat` handling with a test.

## Permissions

All user cache/session dirs must be `0o700` (not `0o755`). Use `os.MkdirAll(..., 0o700)` + `os.CreateTemp` + `os.Rename` atomic pattern from `display_cache.go`.

## Shell Safety

- Never do `"'{q}'"` — use `"{q}"` (double quotes handle `'` in queries) and Go must `strings.Join(args[i+1:], " ")` for multi-word.
- Preview: `bash -c 'exec "$0" --preview-item --manager "$1" -- "$2"' -- {1} {2}` pattern, not bare `{1} {2}`.
- Use `shellQuote` / `shellQuoteIfNeeded` for env values.

## CI

- `test.yml` runs `gofmt -l` + `go vet` + `go test` + `npm test` on `main`+`master`
- `publish-npm.yml` does `Verify build` before `npm publish`, needs `NPM_TOKEN`

## Releasing

```bash
# bump package.json version, then
git tag v1.7.9 && git push origin v1.7.9  # triggers publish-npm.yml
# or push to master/main for prerelease 1.8.1-273.abc123
```

Binaries are prebuilt for `linux/darwin/windows × amd64/arm64` via `scripts/build-go-binaries.sh`.
