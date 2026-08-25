# fpf-cli (`fpf`) 📦

Fast, cross-platform fuzzy package finder for the terminal.

Search packages with `fzf`, preview details, and install/remove/update from one place. Supports 13 package managers across Linux, macOS, and Windows.

![Screenshot](./fpf.png)

## Requirements

- `bash` 4.0+ and `fzf` 0.56.1+ (auto-installed if missing)
- Go 1.24+ (only for building from source)

## Install

```bash
# npm
npm install -g fpf-cli

# bun
bun add -g fpf-cli

# go (from source)
go install github.com/Timmy6942025/fpf-cli/cmd/fpf@latest
```

`npm`/`bun` installs bundle prebuilt Go binaries for Linux/macOS/Windows (`amd64` + `arm64`).
`fpf` launches the packaged Go binary for all runtime behavior. Binaries include SHA256 checksums (`bin/SHA256SUMS`).

## Quick Start

```bash
# Search + install (default action)
fpf ripgrep

# List installed packages
fpf -l

# Remove packages
fpf -R

# Update packages
fpf -U

# Refresh package catalogs only
fpf --refresh
```

By default, `fpf` auto-detects your package manager.

On every OS, default auto mode includes all supported detected managers. If both `bun` and `npm` are available, no-query startup keeps `bun` and skips `npm`; typed query searches include both.

For no-query startup (`fpf`), each manager uses a lighter default query and per-manager result cap to keep startup responsive.

Live reload is enabled by default, with a minimum query length and debounce to reduce lag while typing.

## Supported Managers

- Linux: `apt`, `dnf`, `pacman`, `zypper`, `emerge`
- Windows: `winget`, `choco`, `scoop`
- Cross-platform: `snap`, `flatpak`
- Dev: `npm`, `bun`
- macOS: `brew`

## Manager Override Flags

- `-ap` apt
- `-dn` dnf
- `-pm` pacman
- `-zy` zypper
- `-em` emerge
- `-br` brew
- `-wg` winget
- `-ch` choco
- `-sc` scoop
- `-sn` snap
- `-fp` flatpak
- `-np` npm
- `-bn` bun
- `-m, --manager <name>` full manager name

## Common Options

- `-l, --list-installed` list installed packages
- `-R, --remove` remove selected packages
- `-U, --update` run update/upgrade flow
- `--refresh` refresh package catalogs only
- `-y, --yes` skip confirmation prompts
- `-v, --version` print version and exit
- `-h, --help` show help

## Keybinds

- `ctrl-h` help in preview
- `ctrl-k` keybinds in preview
- `ctrl-/` toggle preview
- `ctrl-n` next selected item
- `ctrl-b` previous selected item

Installed packages are marked with `*` in the result list.

## Notes

- Requires: `bash` + `fzf`
- If `fzf` is missing, `fpf` auto-installs it using a compatible detected manager, then falls back to a release binary download if manager bootstrap fails.
- Root managers (`apt`, `dnf`, `pacman`, `zypper`, `emerge`, `snap`) use `sudo` when needed.
- If Flatpak is detected and Flathub is missing, `fpf` attempts `flatpak remote-add --if-not-exists --user flathub ...` automatically.
- Set `FPF_ASSUME_YES=1` to bypass confirmation prompts in non-interactive flows.
- `FPF_DYNAMIC_RELOAD`: `always` (default), `single`, or `never`
- Live reload uses `change:reload` by default for reliability.
- In auto multi-manager mode, typing (`change`) uses a fast manager subset (`apt`/`bun`-style) while `ctrl-r` triggers a full reload across all detected managers.
- Set `FPF_DYNAMIC_RELOAD_TRANSPORT=ipc` to opt into `--listen` + IPC query notifications on supported `fzf` builds.
- Startup now shows a DynamicProgress-style pre-search bar with concurrent per-manager task text + elapsed time.
- Set `FPF_LOADING_INDICATOR=0` to disable the pre-search loading indicator.
- `FPF_RELOAD_MIN_CHARS`: minimum query length before live reload (default `2`)
- `FPF_RELOAD_DEBOUNCE`: reload debounce seconds (default `0.12`)
- `FPF_DYNAMIC_RELOAD_BYPASS_QUERY_CACHE=1`: bypass query cache during live reloads (default `1` for freshest results); set `0` to prefer cached reloads
- `FPF_DYNAMIC_RELOAD_MANAGERS`: override typing (`change`) live-reload manager scope (examples: `all`, `apt,bun`, `npm`)
- `FPF_MULTI_MANAGER_SEARCH_TIMEOUT_MS`: cap per-manager search command time for multi-manager search/reload; default manager-specific caps favor responsiveness
- `FPF_SEARCH_TIMEOUT_<MANAGER>_MS`: per-manager timeout override in milliseconds (examples: `FPF_SEARCH_TIMEOUT_FLATPAK_MS=1500`, `FPF_SEARCH_TIMEOUT_NPM_MS=1000`)
- `FPF_BUN_ALLOW_NPM_FALLBACK_MULTI=1`: allow bun-to-npm fallback in multi-manager mode (default off to avoid duplicate/slow npm fanout)
- `FPF_PERF_TRACE=1`: print per-stage timing lines to stderr (`manager-resolve`, `search`, `merge`, `mark`, `rank`, `limit`, `fzf`, `dynamic-reload`)
- `FPF_NO_QUERY_INCLUDE_INSTALLED_MARKERS=1`: force installed-marker lookups on no-query multi-manager startup (default skips marker lookups there for faster startup)
- `FPF_ENABLE_QUERY_CACHE`: `auto` (default), `1`, or `0` (`auto` enables query cache for `apt`, `brew`, `pacman`, and `bun`)
- `FPF_QUERY_CACHE_TTL`: global query-cache TTL override seconds for heavy managers (defaults: `apt=180`, `brew=120`, `pacman=180`; set `0` to always refresh)
- `FPF_APT_QUERY_CACHE_TTL`, `FPF_BREW_QUERY_CACHE_TTL`, `FPF_PACMAN_QUERY_CACHE_TTL`: per-manager query-cache TTL overrides
- `FPF_BUN_QUERY_CACHE_TTL`: Bun query-cache TTL (default `300`)
- `FPF_DISABLE_INSTALLED_CACHE=1` disables installed-package marker cache
- `FPF_INSTALLED_CACHE_TTL`: installed-package marker cache freshness window in seconds (default `300`, set `0` to always refresh)

## Linux Live Search Troubleshooting

On Linux, live search (typing to filter results) uses a fast manager subset by default to maintain responsiveness. This is because `npm` searches can be slow due to network latency and repository size. `flatpak` now uses a direct local appstream cache and is included in live search by default.

### Why npm is excluded from live search

The live search feature is designed for instant feedback as you type. Searching npm packages can take several seconds each time, which would cause noticeable lag between your keystrokes and the results updating. To keep the experience snappy, Linux live search excludes the slower `npm` manager by default (`flatpak` is now fast via direct cache).

### How to search all managers while typing

If you need flatpak or npm results to appear as you type, you can override the live search manager scope:

```bash
# Search all managers (including flatpak and npm) while typing
FPF_DYNAMIC_RELOAD_MANAGERS=all fpf ripgrep
```

You can also specify a custom set of managers:

```bash
# Only apt and flatpak while typing
FPF_DYNAMIC_RELOAD_MANAGERS=apt,flatpak fpf ripgrep
```

### Using Ctrl+R for full manager search

By default on Linux, typing uses the fast manager subset (excludes `npm`). To search all detected managers (including `npm`), press `Ctrl+R` to trigger a full reload. The prompt will change to "Loading> " during the search.

### Debugging live search issues

If you're experiencing issues with live search not updating correctly, you can enable debug logging:

```bash
FPF_DEBUG_RELOAD=1 fpf ripgrep
```

This will print debug information to stderr, including:
- The query being searched
- Which managers are being searched
- Result counts
- Any errors encountered

### IPC mode port discovery

When using `FPF_DYNAMIC_RELOAD_TRANSPORT=ipc`, fpf communicates with fzf via its `--listen` flag. The port is automatically discovered from the `FZF_PORT` environment variable set by fzf. If you encounter connection issues, ensure:
- fzf version is 0.56.1 or later
- The `--listen` flag is supported by your fzf version (`fzf --help | grep listen`)
