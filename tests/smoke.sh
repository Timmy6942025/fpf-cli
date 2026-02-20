#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FPF_BIN="${ROOT_DIR}/fpf"
FIXTURE_DIR="${ROOT_DIR}/tests/fixtures"

TMP_DIR="$(mktemp -d)"
MOCK_BIN="${TMP_DIR}/mock-bin"
LOG_FILE="${TMP_DIR}/calls.log"
SUITE_CACHE_ROOT="${TMP_DIR}/cache-root-suite"

cleanup() {
    if [[ "${KEEP_TMP:-0}" == "1" ]]; then
        printf "Keeping temp dir: %s\n" "${TMP_DIR}" >&2
        return
    fi
    rm -rf "${TMP_DIR}"
}
trap cleanup EXIT

mkdir -p "${MOCK_BIN}"
touch "${LOG_FILE}"

cat >"${MOCK_BIN}/mockcmd" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

cmd="$(basename "$0")"
log_file="${FPF_TEST_LOG:?FPF_TEST_LOG must be set}"

fixture_enabled() {
    [[ "${FPF_TEST_FIXTURES:-0}" == "1" ]]
}

fixture_file() {
    local name="$1"
    local root="${FPF_TEST_FIXTURE_DIR:-}"
    [[ -n "${root}" ]] || return 1
    printf "%s/%s" "${root}" "${name}"
}

print_fixture() {
    local name="$1"
    local path
    path="$(fixture_file "${name}")" || return 1
    [[ -f "${path}" ]] || return 1
    cat "${path}"
}

printf "%s %s\n" "${cmd}" "$*" >>"${log_file}"

case "${cmd}" in
    uname)
        if [[ -n "${FPF_TEST_UNAME:-}" ]]; then
            printf "%s\n" "${FPF_TEST_UNAME}"
        else
            /usr/bin/uname "$@"
        fi
        ;;
    sudo)
        if [[ "$#" -gt 0 ]]; then
            exec "$@"
        fi
        ;;
    fzf)
        if [[ "${1:-}" == "--help" ]]; then
            case "${FPF_TEST_FZF_HELP_MODE:-listen}" in
                no-listen)
                    if ! print_fixture "fzf-help-no-listen.txt"; then
                        printf "usage: fzf [options]\n"
                    fi
                    ;;
                *)
                    if ! print_fixture "fzf-help-listen.txt"; then
                        printf "usage: fzf [options]\n"
                        printf "    --listen=SOCKET\n"
                    fi
                    ;;
            esac
            exit 0
        fi

        first_line=""
        IFS= read -r first_line || true
        if [[ -n "${first_line}" ]]; then
            printf "%s\n" "${first_line}"
        fi

        preview_repeat="${FPF_TEST_FZF_PREVIEW_REPEAT:-0}"
        if [[ -n "${first_line}" && "${preview_repeat}" =~ ^[0-9]+$ && "${preview_repeat}" -gt 0 ]]; then
            preview_cmd=""
            for arg in "$@"; do
                case "${arg}" in
                    --preview=*)
                        preview_cmd="${arg#--preview=}"
                        break
                        ;;
                esac
            done

            if [[ -n "${preview_cmd}" ]]; then
                preview_manager=""
                preview_package=""
                IFS=$'\t' read -r preview_manager preview_package _ <<<"${first_line}"
                preview_cmd="${preview_cmd//\{1\}/${preview_manager}}"
                preview_cmd="${preview_cmd//\{2\}/${preview_package}}"

                preview_index=0
                while [[ "${preview_index}" -lt "${preview_repeat}" ]]; do
                    bash -c "${preview_cmd}" >/dev/null 2>&1 || true
                    preview_index=$((preview_index + 1))
                done
            fi
        fi
        ;;
    apt-cache)
        case "${1:-}" in
            dumpavail)
                if fixture_enabled && ! print_fixture "apt-dumpavail.txt"; then
                    cat <<'APT_DUMP'
Package: aptpkg
Description: Apt package for sample-query coverage

APT_DUMP
                elif ! fixture_enabled; then
                    cat <<'APT_DUMP'
Package: aptpkg
Description: Apt package for sample-query coverage

APT_DUMP
                fi
                ;;
            search)
                if fixture_enabled && ! print_fixture "apt-search.txt"; then
                    printf "aptpkg - Apt package\n"
                elif ! fixture_enabled; then
                    printf "aptpkg - Apt package\n"
                fi
                ;;
            show)
                if fixture_enabled && ! print_fixture "apt-show.txt"; then
                    printf "Package: %s\nVersion: 1.0\n" "${2:-aptpkg}"
                elif ! fixture_enabled; then
                    printf "Package: %s\nVersion: 1.0\n" "${2:-aptpkg}"
                fi
                ;;
        esac
        ;;
    dpkg-query)
        if fixture_enabled && ! print_fixture "apt-installed.txt"; then
            printf "aptpkg\t1.0\n"
        elif ! fixture_enabled; then
            printf "aptpkg\t1.0\n"
        fi
        ;;
    dpkg)
        if [[ "${1:-}" == "-L" ]]; then
            printf "/usr/bin/%s\n" "${2:-aptpkg}"
        fi
        ;;
    apt-get)
        if [[ "${FPF_TEST_BOOTSTRAP_FZF:-0}" == "1" ]]; then
            args=" $* "
            if [[ "${args}" == *" install "* && "${args}" == *" fzf "* ]]; then
                ln -sf "${FPF_TEST_MOCKCMD_PATH}" "${FPF_TEST_MOCK_BIN}/fzf"
            fi
        fi
        ;;
    dnf)
        case "${1:-}" in
            -q)
                shift
                if [[ "${1:-}" == "list" && "${2:-}" == "available" ]]; then
                    printf "Available Packages\n"
                    printf "dnfpkg.x86_64 1.0 repo\n"
                elif [[ "${1:-}" == "list" && "${2:-}" == "installed" ]]; then
                    printf "Installed Packages\n"
                    printf "dnfpkg.x86_64 1.0 @repo\n"
                fi
                ;;
            info)
                printf "Name : %s\n" "${2:-dnfpkg}"
                ;;
        esac
        ;;
    rpm)
        if [[ "${1:-}" == "-ql" ]]; then
            printf "/usr/bin/%s\n" "${2:-dnfpkg}"
        fi
        ;;
    pacman)
        case "${1:-}" in
            -Ss)
                printf "core/pacpkg 1.0 [installed]\n"
                printf "    Pacman package\n"
                ;;
            -Q)
                printf "pacpkg 1.0\n"
                ;;
            -Si|-Qi)
                printf "Name            : %s\n" "${2:-pacpkg}"
                ;;
            -Fl|-Ql)
                printf "%s /usr/bin/%s\n" "${2:-pacpkg}" "${2:-pacpkg}"
                ;;
        esac
        ;;
    zypper)
        args=" $* "
        if [[ "${args}" == *" search "* && "${args}" == *" --installed-only "* ]]; then
            printf "i | x | zypkg | x | 1.0 | x | repo\n"
        elif [[ "${args}" == *" search "* ]]; then
            printf "v | x | zypkg | x | 1.0 | x | repo\n"
        elif [[ "${args}" == *" info "* ]]; then
            printf "Information for package %s\n" "${!#}"
        fi
        ;;
    emerge)
        args=" $* "
        if [[ "${args}" == *" --searchdesc "* ]]; then
            printf "*  app-editors/emepkg\n"
            printf "    Description: Emerge package\n"
        elif [[ "${args}" == *" --search "* ]]; then
            printf "*  app-editors/emepkg\n"
            printf "    Description: Emerge package\n"
        fi
        ;;
    qlist)
        printf "app-editors/emepkg\n"
        ;;
    brew)
        case "${1:-}" in
            formulae)
                if fixture_enabled; then
                    if ! print_fixture "brew-formulae.txt"; then
                        printf "aa-tool\n"
                        printf "sample-query\n"
                        printf "brewpkg\n"
                    fi
                else
                    printf "aa-tool\n"
                    printf "sample-query\n"
                    printf "brewpkg\n"
                fi
                ;;
            casks)
                if fixture_enabled; then
                    if ! print_fixture "brew-casks.txt"; then
                        printf "ripgrep-beta\n"
                    fi
                else
                    printf "ripgrep-beta\n"
                fi
                ;;
            search)
                if [[ "${FPF_TEST_MULTI_TOKEN:-0}" == "1" && "${2:-}" == "claude code" ]]; then
                    printf "claude-code-router\n"
                    printf "claude-code\n"
                elif [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" && "${2:-}" == "opencode" ]]; then
                    printf "opencode-tools\n"
                else
                    if fixture_enabled; then
                        if ! print_fixture "brew-search.txt"; then
                            printf "brewpkg\n"
                        fi
                    else
                        printf "brewpkg\n"
                    fi
                fi
                ;;
            list)
                if [[ "${2:-}" == "--versions" ]]; then
                    if fixture_enabled && ! print_fixture "brew-installed.txt"; then
                        printf "brewpkg 1.0\n"
                    elif ! fixture_enabled; then
                        printf "brewpkg 1.0\n"
                    fi
                fi
                ;;
            info)
                if [[ "${FPF_TEST_MULTI_TOKEN:-0}" == "1" ]]; then
                    pkg="${!#}"
                    if [[ "${pkg}" == "claude-code" ]]; then
                        printf "claude-code: stable 1.0\n"
                    else
                        exit 1
                    fi
                elif [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" ]]; then
                    pkg="${!#}"
                    if [[ "${pkg}" == "opencode" ]]; then
                        printf "opencode: stable 1.0\n"
                    else
                        exit 1
                    fi
                else
                    if fixture_enabled; then
                        if ! print_fixture "brew-info.txt"; then
                            printf "brewpkg: stable 1.0\n"
                        fi
                    else
                        printf "brewpkg: stable 1.0\n"
                    fi
                fi
                ;;
        esac
        ;;
    winget)
        case "${1:-}" in
            source)
                if [[ "${2:-}" == "list" && "${FPF_TEST_WINGET_NO_SOURCES:-0}" != "1" ]]; then
                    printf "Name    Arg                               Type\n"
                    printf "------------------------------------------------\n"
                    printf "winget  https://cdn.winget.microsoft.com/cache  Microsoft.Rest\n"
                fi
                ;;
            search)
                if [[ "${FPF_TEST_WINGET_NO_SOURCES:-0}" != "1" ]]; then
                    printf "Name Id Version Source\n"
                    printf '%s\n' '--------------------------------'
                    printf "Visual Studio Code  Microsoft.VisualStudioCode  1.0  winget\n"
                fi
                ;;
            list)
                if [[ "${FPF_TEST_WINGET_NO_SOURCES:-0}" != "1" ]]; then
                    printf "Name Id Version Source\n"
                    printf '%s\n' '--------------------------------'
                    printf "Visual Studio Code  Microsoft.VisualStudioCode  1.0  winget\n"
                fi
                ;;
            show)
                printf "Found package: %s\n" "${3:-Microsoft.VisualStudioCode}"
                ;;
            install|uninstall|upgrade)
                ;;
        esac
        ;;
    choco)
        case "${1:-}" in
            source)
                if [[ "${2:-}" == "list" && "${FPF_TEST_CHOCO_NO_SOURCES:-0}" != "1" ]]; then
                    printf "chocolatey|https://community.chocolatey.org/api/v2/|true|false|0|false|false|24|false\n"
                fi
                ;;
            search)
                if [[ "${FPF_TEST_CHOCO_NO_SOURCES:-0}" != "1" ]]; then
                    printf "chocopkg|1.0\n"
                fi
                ;;
            list)
                if [[ "${FPF_TEST_CHOCO_NO_SOURCES:-0}" != "1" ]]; then
                    printf "chocopkg|1.0\n"
                fi
                ;;
            info)
                printf "Title: chocopkg\n"
                ;;
            install|uninstall|upgrade)
                ;;
        esac
        ;;
    scoop)
        case "${1:-}" in
            bucket)
                if [[ "${2:-}" == "list" && "${FPF_TEST_SCOOP_NO_BUCKETS:-0}" != "1" ]]; then
                    printf "Name  Source\n"
                    printf "main  https://github.com/ScoopInstaller/Main\n"
                fi
                ;;
            search)
                if [[ "${FPF_TEST_SCOOP_NO_BUCKETS:-0}" != "1" ]]; then
                    printf "Name Version Source Updated Info\n"
                    printf "scooppkg 1.0 main now Scoop package\n"
                fi
                ;;
            list)
                if [[ "${FPF_TEST_SCOOP_NO_BUCKETS:-0}" != "1" ]]; then
                    printf "Name Version Source Updated Info\n"
                    printf "scooppkg 1.0 main now installed\n"
                fi
                ;;
            info)
                printf "Name: scooppkg\n"
                ;;
            install|uninstall|update)
                ;;
        esac
        ;;
    snap)
        case "${1:-}" in
            find)
                printf "Name Version Publisher Notes Summary\n"
                printf "snappkg 1.0 pub - Snap package\n"
                ;;
            list)
                printf "Name Version Rev Tracking Publisher Notes\n"
                printf "snappkg 1.0 1 latest/stable pub -\n"
                ;;
            info)
                printf "name: snappkg\n"
                ;;
        esac
        ;;
    flatpak)
        case "${1:-}" in
            remotes|remote-list)
                if [[ "${FPF_TEST_FLATPAK_NO_REMOTES:-0}" != "1" ]]; then
                    printf "flathub\n"
                fi
                ;;
            remote-ls)
                if [[ "${FPF_TEST_FLATPAK_NO_REMOTES:-0}" != "1" ]]; then
                    printf "Application Description\n"
                    printf "org.example.Flat Flatpak package\n"
                fi
                ;;
            search)
                if [[ "${FPF_TEST_FLATPAK_NO_REMOTES:-0}" != "1" ]]; then
                    printf "Application Description\n"
                    printf "org.example.Flat Flatpak package\n"
                fi
                ;;
            list)
                printf "Application Version\n"
                printf "org.example.Flat 1.0\n"
                ;;
            info)
                printf "Ref: app/org.example.Flat/x86_64/stable\n"
                ;;
            remote-info)
                printf "Ref: app/org.example.Flat/x86_64/stable\n"
                ;;
            install|uninstall|update)
                args=" $* "
                if [[ "${FPF_TEST_FLATPAK_USER_FAIL:-0}" == "1" && "${args}" == *" --user "* ]]; then
                    exit 1
                fi
                ;;
        esac
        ;;
    npm)
        case "${1:-}" in
            search)
                if [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" && "${2:-}" == "opencode" ]]; then
                    printf "opencode-tools\tOpenCode helper package\n"
                else
                    printf "npmpkg\tNpm package\n"
                fi
                ;;
            ls)
                if [[ "${FPF_TEST_NPM_SCOPED_LIST:-0}" == "1" ]]; then
                    if [[ "${FPF_TEST_NPM_WINDOWS_PATH:-0}" == "1" ]]; then
                        printf 'C:\\Users\\tester\\AppData\\Roaming\\npm\\node_modules\n'
                        printf 'C:\\Users\\tester\\AppData\\Roaming\\npm\\node_modules\\@opencode-ai\\cli\n'
                    else
                        printf "/usr/lib/node_modules\n"
                        printf "/usr/lib/node_modules/@opencode-ai/cli\n"
                    fi
                else
                    printf "/usr/lib/node_modules\n"
                    printf "/usr/lib/node_modules/npmpkg\n"
                fi
                ;;
            view)
                if [[ "${FPF_TEST_NPM_VIEW_FAIL:-0}" == "1" ]]; then
                    exit 1
                fi
                if [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" ]]; then
                    if [[ "${2:-}" == "opencode" ]]; then
                        printf "name = 'opencode'\n"
                    else
                        exit 1
                    fi
                else
                    printf "name = 'npmpkg'\n"
                fi
                ;;
        esac
        ;;
    bun)
        case "${1:-}" in
            search)
                if [[ "${FPF_TEST_BUN_SEARCH_FAIL:-0}" == "1" ]]; then
                    exit 1
                fi
                if [[ "${FPF_TEST_BUN_REFRESH_SLEEP:-0}" != "0" ]]; then
                    sleep "${FPF_TEST_BUN_REFRESH_SLEEP}"
                fi
                case "${FPF_TEST_BUN_REFRESH_VARIANT:-}" in
                    old)
                        printf "Name Description\n"
                        printf "oldpkg Old generation result\n"
                        exit 0
                        ;;
                    new)
                        printf "Name Description\n"
                        printf "newpkg New generation result\n"
                        exit 0
                        ;;
                esac
                if [[ "${FPF_TEST_MULTI_TOKEN:-0}" == "1" && "${2:-}" == "claude code" ]]; then
                    printf "Name Description\n"
                    printf "claude-code-router Claude Code router extension\n"
                    printf "claude-code Official Claude Code package\n"
                elif [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" && "${2:-}" == "opencode" ]]; then
                    printf "Name Description\n"
                    printf "opencode-tools Bun helper package\n"
                else
                    if fixture_enabled; then
                        if ! print_fixture "bun-search.txt"; then
                            printf "Name Description\n"
                            printf "bunpkg Bun package\n"
                        fi
                    else
                        printf "Name Description\n"
                        printf "bunpkg Bun package\n"
                    fi
                fi
                ;;
            info)
                if [[ "${FPF_TEST_MULTI_TOKEN:-0}" == "1" ]]; then
                    if [[ "${2:-}" == "claude-code" ]]; then
                        printf "name: claude-code\n"
                    else
                        exit 1
                    fi
                elif [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" ]]; then
                    if [[ "${2:-}" == "opencode" ]]; then
                        printf "name: opencode\n"
                    else
                        exit 1
                    fi
                else
                    if fixture_enabled; then
                        if ! print_fixture "bun-info.txt"; then
                            printf "name: bunpkg\n"
                        fi
                    else
                        printf "name: bunpkg\n"
                    fi
                fi
                ;;
            pm)
                if [[ "${2:-}" == "ls" ]]; then
                    if [[ "${FPF_TEST_BUN_PM_FAIL:-0}" == "1" ]]; then
                        exit 1
                    elif [[ "${FPF_TEST_BUN_TREE_LIST:-0}" == "1" ]]; then
                        printf "/Users/test/.bun/install/global node_modules (1)\n"
                        printf "+- opencode-ai@1.2.6\n"
                    elif [[ "${FPF_TEST_BUN_SCOPED_TREE_LIST:-0}" == "1" ]]; then
                        printf "/Users/test/.bun/install/global node_modules (1)\n"
                        printf "+- @openai/codex@0.101.0\n"
                    else
                        if fixture_enabled; then
                            if ! print_fixture "bun-installed.txt"; then
                                printf "Name Version\n"
                                printf "bunpkg 1.0\n"
                            fi
                        else
                            printf "Name Version\n"
                            printf "bunpkg 1.0\n"
                        fi
                    fi
                fi
                ;;
            add|remove|update)
                ;;
        esac
        ;;
    fpf-refresh-signal)
        if [[ -n "${FPF_TEST_CACHE_REFRESH_SIGNAL_FILE:-}" ]]; then
            printf "refresh-complete\n" >>"${FPF_TEST_CACHE_REFRESH_SIGNAL_FILE}"
        fi
        ;;
esac
EOF

chmod +x "${MOCK_BIN}/mockcmd"

for cmd in uname sudo fzf apt-cache dpkg-query dpkg apt-get dnf rpm pacman zypper emerge qlist brew winget choco scoop snap flatpak npm bun fpf-refresh-signal; do
    ln -s "${MOCK_BIN}/mockcmd" "${MOCK_BIN}/${cmd}"
done

export PATH="${MOCK_BIN}:/usr/bin:/bin"
export FPF_TEST_LOG="${LOG_FILE}"
export FPF_TEST_MOCK_BIN="${MOCK_BIN}"
export FPF_TEST_MOCKCMD_PATH="${MOCK_BIN}/mockcmd"
export FPF_TEST_FIXTURE_DIR="${FIXTURE_DIR}"
rm -rf "${SUITE_CACHE_ROOT}"
export FPF_CACHE_DIR="${SUITE_CACHE_ROOT}"

assert_contains() {
    local needle="$1"
    local haystack
    haystack="$(cat "${LOG_FILE}")"
    if [[ "${haystack}" != *"${needle}"* ]]; then
        printf "Expected log to contain: %s\n" "${needle}" >&2
        printf "Actual log:\n%s\n" "${haystack}" >&2
        exit 1
    fi
}

assert_not_contains() {
    local needle="$1"
    local haystack
    haystack="$(cat "${LOG_FILE}")"
    if [[ "${haystack}" == *"${needle}"* ]]; then
        printf "Expected log NOT to contain: %s\n" "${needle}" >&2
        printf "Actual log:\n%s\n" "${haystack}" >&2
        exit 1
    fi
}

assert_logged_exact() {
    local expected_line="$1"
    if ! grep -Fxq -- "${expected_line}" "${LOG_FILE}"; then
        printf "Expected exact log line: %s\n" "${expected_line}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

assert_not_logged_exact() {
    local unexpected_line="$1"
    if grep -Fxq -- "${unexpected_line}" "${LOG_FILE}"; then
        printf "Unexpected exact log line: %s\n" "${unexpected_line}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

assert_output_contains() {
    local output="$1"
    local needle="$2"
    if [[ "${output}" != *"${needle}"* ]]; then
        printf "Expected output to contain: %s\n" "${needle}" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi
}

assert_output_not_contains() {
    local output="$1"
    local needle="$2"
    if [[ "${output}" == *"${needle}"* ]]; then
        printf "Expected output NOT to contain: %s\n" "${needle}" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi
}

assert_file_contains() {
    local file_path="$1"
    local needle="$2"

    if [[ ! -f "${file_path}" ]]; then
        printf "Expected file to exist: %s\n" "${file_path}" >&2
        exit 1
    fi

    if ! grep -Fq -- "${needle}" "${file_path}"; then
        printf "Expected file %s to contain: %s\n" "${file_path}" "${needle}" >&2
        printf "Actual file content:\n%s\n" "$(cat "${file_path}")" >&2
        exit 1
    fi
}

latest_fzf_log_line() {
    grep '^fzf ' "${LOG_FILE}" | tail -n 1 || true
}

assert_fzf_line_contains() {
    local needle="$1"
    local fzf_line
    fzf_line="$(latest_fzf_log_line)"
    if [[ -z "${fzf_line}" || "${fzf_line}" != *"${needle}"* ]]; then
        printf "Expected fzf invocation to contain: %s\n" "${needle}" >&2
        printf "Actual fzf invocation:\n%s\n" "${fzf_line}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

assert_fzf_line_not_contains() {
    local needle="$1"
    local fzf_line
    fzf_line="$(latest_fzf_log_line)"
    if [[ -n "${fzf_line}" && "${fzf_line}" == *"${needle}"* ]]; then
        printf "Expected fzf invocation NOT to contain: %s\n" "${needle}" >&2
        printf "Actual fzf invocation:\n%s\n" "${fzf_line}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

reset_log() {
    : >"${LOG_FILE}"
}

run_search_install_test() {
    local manager="$1"
    local expected="$2"
    reset_log
    printf "y\n" | "${FPF_BIN}" --manager "${manager}" sample-query >/dev/null
    assert_contains "${expected}"
}

run_remove_test() {
    local manager="$1"
    local expected="$2"
    reset_log
    printf "y\n" | "${FPF_BIN}" --manager "${manager}" -R sample-query >/dev/null
    assert_contains "${expected}"
}

run_list_test() {
    local manager="$1"
    local expected="$2"
    reset_log
    "${FPF_BIN}" --manager "${manager}" -l sample-query >/dev/null
    assert_contains "${expected}"
}

run_update_test() {
    local manager="$1"
    local expected="$2"
    reset_log
    printf "y\n" | "${FPF_BIN}" --manager "${manager}" -U >/dev/null
    assert_contains "${expected}"
}

run_fzf_bootstrap_test() {
    reset_log
    rm -f "${MOCK_BIN}/fzf"
    export FPF_TEST_BOOTSTRAP_FZF="1"
    export FPF_TEST_FORCE_FZF_MISSING="1"
    printf "y\n" | "${FPF_BIN}" --manager apt sample-query >/dev/null
    unset FPF_TEST_BOOTSTRAP_FZF
    unset FPF_TEST_FORCE_FZF_MISSING
    ln -sf "${MOCK_BIN}/mockcmd" "${MOCK_BIN}/fzf"

    assert_contains "apt-get install -y fzf"
    assert_contains "apt-get install -y aptpkg"
}

run_macos_auto_scope_test() {
    reset_log
    export FPF_TEST_UNAME="Darwin"
    printf "y\n" | "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_not_contains "npm search sample-query"
}

run_macos_auto_update_test() {
    reset_log
    export FPF_TEST_UNAME="Darwin"
    printf "y\n" | "${FPF_BIN}" -U >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "brew update"
    assert_contains "bun update"
    assert_not_contains "npm update -g"
}

run_macos_no_query_scope_test() {
    reset_log
    export FPF_TEST_UNAME="Darwin"
    printf "n\n" | "${FPF_BIN}" >/dev/null
    unset FPF_TEST_UNAME

    assert_not_contains "npm search aa"
}

run_dynamic_reload_default_auto_test() {
    local uname_value="$1"

    reset_log
    export FPF_TEST_UNAME="${uname_value}"
    printf "n\n" | "${FPF_BIN}" >/dev/null
    unset FPF_TEST_UNAME

    assert_not_contains "--bind=start:reload:"
    assert_fzf_line_contains "--listen=0"
    assert_fzf_line_contains "--bind=change:execute-silent:"
    assert_fzf_line_contains "--ipc-query-notify -- \"{q}\""
    assert_fzf_line_contains "FPF_IPC_FALLBACK_FILE="
    assert_fzf_line_contains "--bind=ctrl-r:reload:"
    assert_fzf_line_not_contains "--bind=change:execute-silent:sleep "
    assert_fzf_line_not_contains "--bind=change:execute-silent:FPF_SKIP_INSTALLED_MARKERS=1"
    assert_fzf_line_not_contains "--bind=change:reload:"
    assert_fzf_line_not_contains "apt-cache search"
    assert_fzf_line_not_contains "brew search"
    assert_fzf_line_not_contains "bun search"
}

run_dynamic_reload_no_listen_fallback_test() {
    reset_log
    export FPF_TEST_FZF_HELP_MODE="no-listen"
    printf "n\n" | "${FPF_BIN}" --manager brew >/dev/null
    unset FPF_TEST_FZF_HELP_MODE

    assert_not_contains "--bind=start:reload:"
    assert_fzf_line_not_contains "--listen=0"
    assert_fzf_line_not_contains "--bind=change:execute-silent:"
    assert_fzf_line_not_contains "--bind=change:reload:"
    assert_fzf_line_not_contains "--ipc-reload"
    assert_fzf_line_contains "--bind=ctrl-r:reload:"
    assert_fzf_line_contains "--feed-search --manager brew -- \"\$q\""
}

run_dynamic_reload_single_mode_single_manager_test() {
    local manager="$1"

    reset_log
    printf "n\n" | FPF_DYNAMIC_RELOAD=single "${FPF_BIN}" --manager "${manager}" >/dev/null

    assert_not_contains "--bind=start:reload:"
    assert_fzf_line_contains "--listen=0"
    assert_fzf_line_contains "--bind=change:execute-silent:"
    assert_fzf_line_contains "--ipc-query-notify -- \"{q}\""
    assert_fzf_line_contains "--bind=ctrl-r:reload:"
}

run_dynamic_reload_single_mode_multi_manager_test() {
    local uname_value="$1"

    reset_log
    export FPF_TEST_UNAME="${uname_value}"
    printf "n\n" | FPF_DYNAMIC_RELOAD=single "${FPF_BIN}" >/dev/null
    unset FPF_TEST_UNAME

    assert_not_contains "--bind=start:reload:"
    assert_fzf_line_not_contains "--listen=0"
    assert_fzf_line_not_contains "--bind=change:execute-silent:"
    assert_fzf_line_not_contains "--bind=change:reload:"
    assert_fzf_line_not_contains "--bind=ctrl-r:reload:"
}

run_dynamic_reload_force_never_test() {
    local uname_value="$1"

    reset_log
    export FPF_TEST_UNAME="${uname_value}"
    printf "n\n" | FPF_DYNAMIC_RELOAD=never "${FPF_BIN}" >/dev/null
    unset FPF_TEST_UNAME

    assert_not_contains "--bind=start:reload:"
    assert_not_contains "--bind=change:reload:"
    assert_not_contains "--feed-search"
}

run_dynamic_reload_override_test() {
    local manager="$1"

    reset_log
    printf "n\n" | "${FPF_BIN}" --manager "${manager}" >/dev/null

    assert_not_contains "--bind=start:reload:"
    assert_fzf_line_contains "--listen=0"
    assert_fzf_line_contains "--bind=change:execute-silent:"
    assert_fzf_line_contains "--ipc-query-notify -- \"{q}\""
    assert_fzf_line_contains "FPF_IPC_MANAGER_OVERRIDE=${manager}"
    assert_fzf_line_contains "--bind=ctrl-r:reload:"
    assert_fzf_line_not_contains "--bind=change:execute-silent:sleep "
    assert_fzf_line_not_contains "--bind=change:execute-silent:FPF_SKIP_INSTALLED_MARKERS=1"
}

run_fzf_ui_regression_guard_test() {
    reset_log
    printf "n\n" | "${FPF_BIN}" --manager brew sample-query >/dev/null

    assert_contains "--preview-window=55%:wrap:border-sharp"
    assert_not_contains "--preview-window=55%:wrap:border-sharp:hidden"
    assert_contains "--bind=focus:transform-preview-label:echo [{1}] {2}"
    assert_contains "fzf -q sample-query -m -e"

    reset_log
    printf "n\n" | "${FPF_BIN}" --manager brew >/dev/null

    assert_contains "--listen=0"
    assert_contains "--bind=change:execute-silent:"
    assert_contains "--ipc-query-notify -- \"{q}\""
    assert_contains "--bind=ctrl-r:reload:"
    assert_not_contains "--bind=change:reload:"
}

run_feed_search_manager_mix_test() {
    local uname_value="$1"
    local output=""

    export FPF_TEST_UNAME="${uname_value}"
    output="$(${FPF_BIN} --feed-search -- sample-query)"
    unset FPF_TEST_UNAME

    assert_output_contains "${output}" $'apt\t'
    assert_output_contains "${output}" $'dnf\t'
    assert_output_contains "${output}" $'pacman\t'
    assert_output_contains "${output}" $'zypper\t'
    assert_output_contains "${output}" $'emerge\t'
    assert_output_contains "${output}" $'brew\t'
    assert_output_contains "${output}" $'winget\t'
    assert_output_contains "${output}" $'choco\t'
    assert_output_contains "${output}" $'scoop\t'
    assert_output_contains "${output}" $'snap\t'
    assert_output_contains "${output}" $'flatpak\t'
    assert_output_contains "${output}" $'bun\t'
    assert_output_not_contains "${output}" $'npm\t'
}

run_fixture_catalog_feed_search_test() {
    local output=""

    export FPF_TEST_FIXTURES="1"

    output="$(${FPF_BIN} --manager apt --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'apt\tripgrep\t'
    assert_output_contains "${output}" $'apt\tripgrep-all\t'

    output="$(${FPF_BIN} --manager brew --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'brew\tripgrep\t'
    assert_output_contains "${output}" $'brew\tripgrep-beta\t'

    output="$(${FPF_BIN} --manager bun --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'bun\tripgrep\t'
    assert_output_contains "${output}" $'bun\tripgrep-runner\t'

    unset FPF_TEST_FIXTURES
}

run_feed_search_merged_tsv_contract_test() {
    local output=""
    local output_repeat=""
    local cache_root="${TMP_DIR}/cache-root-merged-tsv"
    local key_count=0
    local row_count=0

    reset_log
    rm -rf "${cache_root}"

    output="$(FPF_TEST_FIXTURES="1" FPF_TEST_UNAME="Linux" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --feed-search -- ripgrep)"
    output_repeat="$(FPF_TEST_FIXTURES="1" FPF_TEST_UNAME="Linux" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --feed-search -- ripgrep)"

    if [[ -z "${output}" ]]; then
        printf "Expected merged feed output to be non-empty\n" >&2
        exit 1
    fi

    if ! printf "%s\n" "${output}" | awk -F'\t' 'NF != 3 { exit 1 } END { exit 0 }'; then
        printf "Expected merged feed output rows to contain exactly 3 TSV columns\n" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi

    if ! printf "%s\n" "${output}" | awk -F'\t' '$3 !~ /^\* / && $3 !~ /^  / { exit 1 } END { exit 0 }'; then
        printf "Expected merged feed third column to include installed marker prefix\n" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi

    key_count="$(printf "%s\n" "${output}" | awk -F'\t' '{ print $1 "\t" $2 }' | sort -u | wc -l | tr -d ' ')"
    row_count="$(printf "%s\n" "${output}" | awk 'NF > 0' | wc -l | tr -d ' ')"
    if [[ "${key_count}" -ne "${row_count}" ]]; then
        printf "Expected merged feed output to dedupe by manager+package key\n" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi

    if [[ "${output}" != "${output_repeat}" ]]; then
        printf "Expected merged feed output ordering to be stable across warm rebuilds\n" >&2
        printf "First output:\n%s\n" "${output}" >&2
        printf "Second output:\n%s\n" "${output_repeat}" >&2
        exit 1
    fi
}

run_feed_search_installed_marker_path_test() {
    local output=""
    local cache_root="${TMP_DIR}/cache-root-marker-path"
    local dpkg_query_count=0
    local brew_list_count=0
    local bun_pm_count=0

    reset_log
    rm -rf "${cache_root}"

    output="$(FPF_TEST_FIXTURES="1" FPF_TEST_UNAME="Linux" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'apt\tripgrep\t* '
    assert_output_contains "${output}" $'brew\tripgrep\t* '
    assert_output_contains "${output}" $'bun\tripgrep\t* '

    dpkg_query_count="$(grep -c '^dpkg-query -W -f=' "${LOG_FILE}" || true)"
    brew_list_count="$(grep -c '^brew list --versions$' "${LOG_FILE}" || true)"
    bun_pm_count="$(grep -c '^bun pm ls --global$' "${LOG_FILE}" || true)"

    if [[ "${dpkg_query_count}" -ne 1 || "${brew_list_count}" -ne 1 || "${bun_pm_count}" -ne 1 ]]; then
        printf "Expected exactly one installed lookup per manager during rebuild (apt=%s brew=%s bun=%s)\n" "${dpkg_query_count}" "${brew_list_count}" "${bun_pm_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_feed_search_skip_marker_warm_path_test() {
    local output=""
    local cache_root="${TMP_DIR}/cache-root-skip-marker"

    reset_log
    rm -rf "${cache_root}"

    output="$(FPF_TEST_FIXTURES="1" FPF_TEST_UNAME="Linux" FPF_SKIP_INSTALLED_MARKERS=1 FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'apt\tripgrep\t  '
    assert_output_contains "${output}" $'brew\tripgrep\t  '
    assert_output_contains "${output}" $'bun\tripgrep\t  '

    output="$(FPF_TEST_FIXTURES="1" FPF_TEST_UNAME="Linux" FPF_SKIP_INSTALLED_MARKERS=1 FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --feed-search -- rg)"
    if ! printf "%s\n" "${output}" | awk -F'\t' '$3 !~ /^  / { exit 1 } END { exit 0 }'; then
        printf "Expected warm skip-marker rows to keep uninstalled marker prefix in column 3\n" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi

    assert_not_contains "dpkg-query -W -f='\${binary:Package}\\t\${Version}\\n'"
    assert_not_contains "brew list --versions"
    assert_not_contains "bun pm ls --global"
}

run_fzf_listen_help_simulation_test() {
    local help_output=""

    export FPF_TEST_FZF_HELP_MODE="listen"
    help_output="$(fzf --help)"
    assert_output_contains "${help_output}" "--listen"

    export FPF_TEST_FZF_HELP_MODE="no-listen"
    help_output="$(fzf --help)"
    assert_output_not_contains "${help_output}" "--listen"

    unset FPF_TEST_FZF_HELP_MODE
}

run_cache_refresh_signal_simulation_test() {
    local signal_file="${TMP_DIR}/cache-refresh.signal"
    rm -f "${signal_file}"

    export FPF_TEST_CACHE_REFRESH_SIGNAL_FILE="${signal_file}"
    fpf-refresh-signal
    unset FPF_TEST_CACHE_REFRESH_SIGNAL_FILE

    assert_file_contains "${signal_file}" "refresh-complete"
}

run_multi_token_exact_priority_test() {
    local output=""
    local top_four=""

    export FPF_TEST_MULTI_TOKEN="1"
    output="$(${FPF_BIN} --feed-search -- 'claude code')"
    unset FPF_TEST_MULTI_TOKEN

    top_four="$(printf "%s\n" "${output}" | awk 'NR <= 4')"
    assert_output_contains "${top_four}" $'brew\tclaude-code\t'
    assert_output_contains "${top_four}" $'bun\tclaude-code\t'

    first_line="$(printf "%s\n" "${output}" | awk 'NR == 1 {print; exit}')"
    if [[ "${first_line}" == *$'claude-code-router\t'* ]]; then
        printf "Expected exact package ahead of router variant.\n" >&2
        printf "Actual output:\n%s\n" "${output}" >&2
        exit 1
    fi
}

run_installed_cache_test() {
    reset_log
    local cache_root="${TMP_DIR}/cache-root"
    rm -rf "${cache_root}"

    FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager brew --feed-search -- sample-query >/dev/null
    FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager brew --feed-search -- other-query >/dev/null

    assert_file_contains "${cache_root}/catalog/brew.tsv" "brewpkg"
    assert_file_contains "${cache_root}/meta/catalog/brew.tsv.meta" "format_version=1"
    assert_file_contains "${cache_root}/meta/catalog/brew.tsv.meta" "created_at="
    assert_file_contains "${cache_root}/meta/catalog/brew.tsv.meta" "fingerprint=1|brew|"
    assert_file_contains "${cache_root}/meta/catalog/brew.tsv.meta" "item_count=1"

    local brew_list_count
    brew_list_count="$(grep -c '^brew list --versions$' "${LOG_FILE}" || true)"
    if [[ "${brew_list_count}" -ne 1 ]]; then
        printf "Expected brew installed list to be cached (1 call), got %s\n" "${brew_list_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_apt_catalog_cache_rebuild_test() {
    reset_log
    local cache_root="${TMP_DIR}/cache-root-apt-catalog"
    rm -rf "${cache_root}"

    FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager apt --feed-search -- ripgrep >/dev/null
    FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager apt --feed-search -- fd >/dev/null

    local apt_dumpavail_count
    apt_dumpavail_count="$(grep -c '^apt-cache dumpavail$' "${LOG_FILE}" || true)"
    if [[ "${apt_dumpavail_count}" -ne 1 ]]; then
        printf "Expected apt catalog rebuild once (apt-cache dumpavail), got %s\n" "${apt_dumpavail_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi

    local apt_search_count
    apt_search_count="$(grep -c '^apt-cache search -- ' "${LOG_FILE}" || true)"
    if [[ "${apt_search_count}" -ne 0 ]]; then
        printf "Expected apt warm path to avoid apt-cache search, got %s\n" "${apt_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_brew_catalog_cache_rebuild_test() {
    reset_log
    local cache_root="${TMP_DIR}/cache-root-brew-catalog"
    rm -rf "${cache_root}"

    FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager brew --feed-search -- ripgrep >/dev/null
    FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager brew --feed-search -- fd >/dev/null

    local brew_formulae_count
    brew_formulae_count="$(grep -c '^brew formulae$' "${LOG_FILE}" || true)"
    if [[ "${brew_formulae_count}" -ne 1 ]]; then
        printf "Expected brew catalog rebuild once (brew formulae), got %s\n" "${brew_formulae_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi

    local brew_casks_count
    brew_casks_count="$(grep -c '^brew casks$' "${LOG_FILE}" || true)"
    if [[ "${brew_casks_count}" -ne 1 ]]; then
        printf "Expected brew catalog rebuild once (brew casks), got %s\n" "${brew_casks_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi

    local brew_search_count
    brew_search_count="$(grep -c '^brew search ' "${LOG_FILE}" || true)"
    if [[ "${brew_search_count}" -ne 0 ]]; then
        printf "Expected brew warm path to avoid brew search, got %s\n" "${brew_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_query_cache_layout_test() {
    reset_log
    local cache_root="${TMP_DIR}/cache-root-query"
    local flags="query_limit=0;per_manager_limit=40;no_query_limit=120;no_query_npm_limit=120"
    local fingerprint="1|brew|linux|sample-query|${flags}"
    local checksum
    checksum="$(printf "%s" "${fingerprint}" | cksum | awk '{ print $1 }')"
    rm -rf "${cache_root}"

    FPF_ENABLE_QUERY_CACHE="1" FPF_CACHE_DIR="${cache_root}" FPF_TEST_UNAME="Linux" "${FPF_BIN}" --manager brew --feed-search -- sample-query >/dev/null
    FPF_ENABLE_QUERY_CACHE="1" FPF_CACHE_DIR="${cache_root}" FPF_TEST_UNAME="Linux" "${FPF_BIN}" --manager brew --feed-search -- sample-query >/dev/null

    assert_file_contains "${cache_root}/query/brew/${checksum}.tsv" "sample-query"
    assert_file_contains "${cache_root}/meta/query/brew/${checksum}.tsv.meta" "format_version=1"
    assert_file_contains "${cache_root}/meta/query/brew/${checksum}.tsv.meta" "created_at="
    assert_file_contains "${cache_root}/meta/query/brew/${checksum}.tsv.meta" "fingerprint=${fingerprint}"
    assert_file_contains "${cache_root}/meta/query/brew/${checksum}.tsv.meta" "item_count="

    local brew_search_count
    brew_search_count="$(grep -c '^brew search sample-query$' "${LOG_FILE}" || true)"
    if [[ "${brew_search_count}" -ne 0 ]]; then
        printf "Expected brew catalog warm path to avoid brew search, got %s\n" "${brew_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_winget_id_parsing_test() {
    reset_log
    printf "y\n" | "${FPF_BIN}" --manager winget sample-query >/dev/null
    assert_contains "winget install --id Microsoft.VisualStudioCode --exact"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager winget -R sample-query >/dev/null
    assert_contains "winget uninstall --id Microsoft.VisualStudioCode --exact --source winget"

    reset_log
    "${FPF_BIN}" --manager winget -l sample-query >/dev/null
    assert_contains "winget show --id Microsoft.VisualStudioCode --exact"
}

run_bun_global_scope_guard_test() {
    reset_log
    printf "y\n" | "${FPF_BIN}" --manager bun -R sample-query >/dev/null
    assert_logged_exact "bun remove --global bunpkg"
    assert_not_logged_exact "bun remove bunpkg"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager bun -U >/dev/null
    assert_logged_exact "bun update --global"
    assert_not_logged_exact "bun update"
}

run_bun_search_single_call_test() {
    reset_log
    printf "n\n" | "${FPF_BIN}" --manager bun sample-query >/dev/null

    local bun_search_count
    bun_search_count="$(grep -c '^bun search sample-query$' "${LOG_FILE}" || true)"
    if [[ "${bun_search_count}" -gt 1 ]]; then
        printf "Expected at most one bun search call, got %s\n" "${bun_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_bun_query_cache_warm_immediate_test() {
    local cache_root="${TMP_DIR}/cache-root-bun-query"
    local output=""

    rm -rf "${cache_root}"

    printf "n\n" | FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager bun ripgrep >/dev/null

    reset_log
    output="$(FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=900 "${FPF_BIN}" --manager bun --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'bun\tripgrep\t'

    local bun_search_count
    bun_search_count="$(grep -c '^bun search ripgrep$' "${LOG_FILE}" || true)"
    if [[ "${bun_search_count}" -ne 0 ]]; then
        printf "Expected bun warm query cache path to avoid bun search, got %s\n" "${bun_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_bun_refresh_failure_fallback_test() {
    local cache_root="${TMP_DIR}/cache-root-bun-refresh-failure"
    local output=""
    local meta_file

    rm -rf "${cache_root}"

    printf "n\n" | FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" --manager bun ripgrep >/dev/null

    reset_log
    output="$(FPF_TEST_FIXTURES="1" FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FPF_BUN_TEST_SYNC_REFRESH=1 FPF_TEST_BUN_SEARCH_FAIL=1 "${FPF_BIN}" --manager bun --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'bun\tripgrep\t'

    meta_file="$(printf '%s\n' "${cache_root}"/meta/query/bun/*.meta | awk 'NR==1 {print; exit}')"
    assert_file_contains "${meta_file}" "refresh_status=error"
    assert_file_contains "${meta_file}" "last_error_at="
}

run_bun_ttl_expiration_schedules_refresh_test() {
    local cache_root="${TMP_DIR}/cache-root-bun-ttl-schedule"
    local output=""
    local state_file=""

    rm -rf "${cache_root}"

    FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FPF_BUN_TEST_SYNC_REFRESH=1 FPF_BUN_REFRESH_IDLE=0 FPF_TEST_BUN_REFRESH_VARIANT=old "${FPF_BIN}" --manager bun --feed-search -- ripgrep >/dev/null

    state_file="$(printf '%s\n' "${cache_root}"/state/query/bun/*.generation | awk 'NR==1 {print; exit}')"
    assert_file_contains "${state_file}" "1"

    reset_log
    output="$(FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FZF_PORT=9999 FPF_TEST_BUN_REFRESH_VARIANT=new "${FPF_BIN}" --manager bun --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'bun\toldpkg\t'
    assert_output_not_contains "${output}" $'bun\tnewpkg\t'

    assert_file_contains "${state_file}" "2"
}

run_bun_corrupt_cache_fallback_test() {
    local cache_root="${TMP_DIR}/cache-root-bun-corrupt-fallback"
    local cache_file=""
    local meta_file=""
    local output=""
    local bun_search_count=0

    rm -rf "${cache_root}"

    FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FPF_BUN_TEST_SYNC_REFRESH=1 FPF_BUN_REFRESH_IDLE=0 FPF_TEST_BUN_REFRESH_VARIANT=old "${FPF_BIN}" --manager bun --feed-search -- ripgrep >/dev/null

    cache_file="$(printf '%s\n' "${cache_root}"/query/bun/*.tsv | awk 'NR==1 {print; exit}')"
    meta_file="$(printf '%s\n' "${cache_root}"/meta/query/bun/*.meta | awk 'NR==1 {print; exit}')"

    printf "corrupt-cache-line-without-tabs\n" >"${cache_file}"

    reset_log
    output="$(FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FPF_BUN_TEST_SYNC_REFRESH=1 FPF_BUN_REFRESH_IDLE=0 FPF_TEST_BUN_REFRESH_VARIANT=new "${FPF_BIN}" --manager bun --feed-search -- ripgrep)"

    assert_output_contains "${output}" $'bun\tnewpkg\t'
    assert_output_not_contains "${output}" $'bun\tcorrupt-cache-line-without-tabs\t'
    assert_file_contains "${meta_file}" "refresh_status=success"

    bun_search_count="$(grep -c '^bun search ripgrep$' "${LOG_FILE}" || true)"
    if [[ "${bun_search_count}" -ne 1 ]]; then
        printf "Expected corrupt bun cache fallback to rebuild exactly once, got %s\n" "${bun_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_bun_generation_ordering_test() {
    local cache_root="${TMP_DIR}/cache-root-bun-generation"
    local meta_file=""
    local key=""
    local fingerprint=""
    local stale_worker_search_count=0
    local output=""

    rm -rf "${cache_root}"

    FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FPF_BUN_TEST_SYNC_REFRESH=1 FPF_BUN_REFRESH_IDLE=0 FPF_TEST_BUN_REFRESH_VARIANT=old "${FPF_BIN}" --manager bun --feed-search -- ripgrep >/dev/null
    FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 FPF_BUN_TEST_SYNC_REFRESH=1 FPF_BUN_REFRESH_IDLE=0 FPF_TEST_BUN_REFRESH_VARIANT=new "${FPF_BIN}" --manager bun --feed-search -- ripgrep >/dev/null

    meta_file="$(printf '%s\n' "${cache_root}"/meta/query/bun/*.meta | awk 'NR==1 {print; exit}')"
    key="${meta_file#${cache_root}/meta/}"
    key="${key%.meta}"
    fingerprint="$(awk -F'=' '$1=="fingerprint" { print substr($0, index($0, "=") + 1); exit }' "${meta_file}")"

    reset_log
    FPF_CACHE_DIR="${cache_root}" \
        FPF_BUN_REFRESH_IDLE=0 \
        FPF_TEST_BUN_REFRESH_VARIANT=old \
        FPF_BUN_REFRESH_KEY="${key}" \
        FPF_BUN_REFRESH_FINGERPRINT="${fingerprint}" \
        FPF_BUN_REFRESH_GENERATION=1 \
        "${FPF_BIN}" --bun-refresh-worker --manager bun -- ripgrep >/dev/null

    stale_worker_search_count="$(grep -c '^bun search ripgrep$' "${LOG_FILE}" || true)"
    if [[ "${stale_worker_search_count}" -ne 0 ]]; then
        printf "Expected stale bun generation worker to skip search entirely, got %s\n" "${stale_worker_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi

    output="$(FPF_CACHE_DIR="${cache_root}" FPF_BUN_QUERY_CACHE_TTL=900 "${FPF_BIN}" --manager bun --feed-search -- ripgrep)"
    assert_output_contains "${output}" $'bun\tnewpkg\t'
    assert_output_not_contains "${output}" $'bun\toldpkg\t'
}

run_dynamic_reload_override_auto_parity_test() {
    local auto_fzf_line=""
    local override_fzf_line=""

    reset_log
    printf "n\n" | "${FPF_BIN}" >/dev/null
    auto_fzf_line="$(latest_fzf_log_line)"
    if [[ -z "${auto_fzf_line}" ]]; then
        printf "Expected auto mode fzf invocation to exist for parity check\n" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi

    reset_log
    printf "n\n" | "${FPF_BIN}" --manager bun >/dev/null
    override_fzf_line="$(latest_fzf_log_line)"
    if [[ -z "${override_fzf_line}" ]]; then
        printf "Expected manager override fzf invocation to exist for parity check\n" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi

    if [[ "${auto_fzf_line}" != *"--listen=0"* || "${override_fzf_line}" != *"--listen=0"* ]]; then
        printf "Expected both auto and override paths to use listen mode\n" >&2
        printf "auto: %s\n" "${auto_fzf_line}" >&2
        printf "override: %s\n" "${override_fzf_line}" >&2
        exit 1
    fi

    if [[ "${auto_fzf_line}" != *"--bind=change:execute-silent:"* || "${override_fzf_line}" != *"--bind=change:execute-silent:"* ]]; then
        printf "Expected both auto and override paths to keep change:execute-silent bind\n" >&2
        printf "auto: %s\n" "${auto_fzf_line}" >&2
        printf "override: %s\n" "${override_fzf_line}" >&2
        exit 1
    fi

    if [[ "${auto_fzf_line}" != *"--ipc-query-notify -- \"{q}\""* || "${override_fzf_line}" != *"--ipc-query-notify -- \"{q}\""* ]]; then
        printf "Expected both auto and override paths to keep ipc-query-notify bind\n" >&2
        printf "auto: %s\n" "${auto_fzf_line}" >&2
        printf "override: %s\n" "${override_fzf_line}" >&2
        exit 1
    fi

    if [[ "${auto_fzf_line}" != *"--bind=ctrl-r:reload:"* || "${override_fzf_line}" != *"--bind=ctrl-r:reload:"* ]]; then
        printf "Expected both auto and override paths to keep ctrl-r reload bind\n" >&2
        printf "auto: %s\n" "${auto_fzf_line}" >&2
        printf "override: %s\n" "${override_fzf_line}" >&2
        exit 1
    fi

    if [[ "${auto_fzf_line}" == *"--bind=change:reload:"* || "${override_fzf_line}" == *"--bind=change:reload:"* ]]; then
        printf "Expected both auto and override paths to avoid change:reload bind\n" >&2
        printf "auto: %s\n" "${auto_fzf_line}" >&2
        printf "override: %s\n" "${override_fzf_line}" >&2
        exit 1
    fi
}

run_bun_tree_installed_remove_test() {
    reset_log
    export FPF_TEST_BUN_TREE_LIST="1"
    printf "y\n" | "${FPF_BIN}" --manager bun -R opencode-ai >/dev/null
    unset FPF_TEST_BUN_TREE_LIST

    assert_logged_exact "bun remove --global opencode-ai"
}

run_bun_scoped_tree_remove_test() {
    reset_log
    export FPF_TEST_BUN_SCOPED_TREE_LIST="1"
    printf "y\n" | "${FPF_BIN}" --manager bun -R "@openai/codex" >/dev/null
    unset FPF_TEST_BUN_SCOPED_TREE_LIST

    assert_logged_exact "bun remove --global @openai/codex"
}

run_npm_scoped_remove_test() {
    reset_log
    export FPF_TEST_NPM_SCOPED_LIST="1"
    printf "y\n" | "${FPF_BIN}" --manager npm -R "@opencode-ai/cli" >/dev/null
    unset FPF_TEST_NPM_SCOPED_LIST

    assert_logged_exact "npm uninstall -g @opencode-ai/cli"
}

run_npm_scoped_remove_windows_path_test() {
    reset_log
    export FPF_TEST_NPM_SCOPED_LIST="1"
    export FPF_TEST_NPM_WINDOWS_PATH="1"
    printf "y\n" | "${FPF_BIN}" --manager npm -R "@opencode-ai/cli" >/dev/null
    unset FPF_TEST_NPM_WINDOWS_PATH
    unset FPF_TEST_NPM_SCOPED_LIST

    assert_logged_exact "npm uninstall -g @opencode-ai/cli"
}

run_bun_fallback_npm_scoped_remove_test() {
    reset_log
    export FPF_TEST_BUN_PM_FAIL="1"
    export FPF_TEST_NPM_SCOPED_LIST="1"
    printf "y\n" | "${FPF_BIN}" --manager bun -R "@opencode-ai/cli" >/dev/null
    unset FPF_TEST_NPM_SCOPED_LIST
    unset FPF_TEST_BUN_PM_FAIL

    assert_logged_exact "bun remove --global @opencode-ai/cli"
}

run_flatpak_scope_fallback_test() {
    export FPF_TEST_FLATPAK_USER_FAIL="1"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager flatpak sample-query >/dev/null
    assert_logged_exact "flatpak install -y --user flathub org.example.Flat"
    assert_logged_exact "flatpak install -y flathub org.example.Flat"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager flatpak -R sample-query >/dev/null
    assert_logged_exact "flatpak uninstall -y --user org.example.Flat"
    assert_logged_exact "flatpak uninstall -y org.example.Flat"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager flatpak -U >/dev/null
    assert_logged_exact "flatpak update -y --user"
    assert_logged_exact "flatpak update -y"

    unset FPF_TEST_FLATPAK_USER_FAIL
}

run_flatpak_no_query_catalog_test() {
    reset_log
    printf "n\n" | "${FPF_BIN}" --manager flatpak >/dev/null

    assert_contains "flatpak remote-ls --app --columns=application,description flathub"
    assert_not_contains "flatpak search --columns=application,description a"
}

run_flatpak_no_remote_config_error_test() {
    local output=""

    reset_log
    export FPF_TEST_FLATPAK_NO_REMOTES="1"
    output="$("${FPF_BIN}" --manager flatpak 2>&1 || true)"
    unset FPF_TEST_FLATPAK_NO_REMOTES

    assert_output_contains "${output}" "Flatpak has no remotes configured"
    assert_output_contains "${output}" "flatpak remote-add --if-not-exists --user flathub"
    assert_contains "flatpak remotes --columns=name"
}

run_winget_no_source_config_error_test() {
    local output=""

    reset_log
    export FPF_TEST_WINGET_NO_SOURCES="1"
    output="$(${FPF_BIN} --manager winget 2>&1 || true)"
    unset FPF_TEST_WINGET_NO_SOURCES

    assert_output_contains "${output}" "WinGet source 'winget' is not configured"
    assert_output_contains "${output}" "winget source reset --force"
    assert_contains "winget source list"
}

run_choco_no_source_config_error_test() {
    local output=""

    reset_log
    export FPF_TEST_CHOCO_NO_SOURCES="1"
    output="$(${FPF_BIN} --manager choco 2>&1 || true)"
    unset FPF_TEST_CHOCO_NO_SOURCES

    assert_output_contains "${output}" "Chocolatey has no package sources configured"
    assert_output_contains "${output}" "choco source add -n=chocolatey"
    assert_contains "choco source list --limit-output"
}

run_scoop_no_bucket_config_error_test() {
    local output=""

    reset_log
    export FPF_TEST_SCOOP_NO_BUCKETS="1"
    output="$(${FPF_BIN} --manager scoop 2>&1 || true)"
    unset FPF_TEST_SCOOP_NO_BUCKETS

    assert_output_contains "${output}" "Scoop has no buckets configured"
    assert_output_contains "${output}" "scoop bucket add main"
    assert_contains "scoop bucket list"
}

run_windows_auto_scope_test() {
    reset_log
    export FPF_TEST_UNAME="MINGW64_NT-10.0"
    printf "y\n" | "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "winget search sample-query --source winget"
    assert_contains "choco search sample-query --limit-output"
    assert_contains "scoop search sample-query"
    assert_not_contains "npm search sample-query"
}

run_windows_auto_update_test() {
    reset_log
    export FPF_TEST_UNAME="MINGW64_NT-10.0"
    printf "y\n" | "${FPF_BIN}" -U >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "winget upgrade --all --source winget"
    assert_contains "choco upgrade all -y"
    assert_contains "scoop update"
    assert_contains "bun update"
    assert_not_contains "npm update -g"
}

run_exact_lookup_recovery_test() {
    reset_log
    export FPF_TEST_EXACT_LOOKUP="1"
    printf "y\n" | "${FPF_BIN}" --manager brew opencode >/dev/null
    assert_contains "brew info --formula opencode"
    assert_contains "brew install opencode"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager npm opencode >/dev/null
    assert_contains "npm search opencode --searchlimit"
    assert_contains "npm view opencode name"
    assert_contains "npm install -g opencode"

    reset_log
    export FPF_TEST_BUN_SEARCH_FAIL="1"
    local bun_cache_root="${TMP_DIR}/cache-root-bun-exact-recovery"
    rm -rf "${bun_cache_root}"
    printf "y\n" | FPF_CACHE_DIR="${bun_cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 "${FPF_BIN}" --manager bun opencode >/dev/null
    unset FPF_TEST_BUN_SEARCH_FAIL
    assert_contains "bun search opencode"
    assert_contains "npm search opencode --searchlimit"
    assert_contains "bun info opencode"
    assert_contains "bun add -g opencode"
    unset FPF_TEST_EXACT_LOOKUP
}

run_bun_exact_lookup_without_npm_view_test() {
    reset_log
    export FPF_TEST_EXACT_LOOKUP="1"
    export FPF_TEST_NPM_VIEW_FAIL="1"
    local bun_cache_root="${TMP_DIR}/cache-root-bun-exact-npm-view"
    rm -rf "${bun_cache_root}"
    printf "y\n" | FPF_CACHE_DIR="${bun_cache_root}" FPF_BUN_QUERY_CACHE_TTL=0 "${FPF_BIN}" --manager bun opencode >/dev/null
    unset FPF_TEST_NPM_VIEW_FAIL
    unset FPF_TEST_EXACT_LOOKUP

    assert_contains "bun search opencode"
    assert_contains "bun info opencode"
    assert_logged_exact "bun add -g opencode"
}

run_all_manager_default_scope_test() {
    local uname_value="$1"
    local cache_root="${TMP_DIR}/cache-root-all-scope-${uname_value}"

    reset_log
    rm -rf "${cache_root}"
    export FPF_TEST_UNAME="${uname_value}"
    printf "n\n" | FPF_CACHE_DIR="${cache_root}" "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "apt-cache dumpavail"
    assert_contains "dnf -q list available"
    assert_contains "pacman -Ss -- sample-query"
    assert_contains "zypper --non-interactive --quiet search --details --type package sample-query"
    assert_contains "emerge --searchdesc --color=n sample-query"
    assert_contains "winget search sample-query --source winget"
    assert_contains "choco search sample-query --limit-output"
    assert_contains "scoop search sample-query"
    assert_contains "snap find sample-query"
    assert_contains "flatpak search --columns=application,description sample-query"
    assert_not_contains "npm search sample-query --searchlimit"
}

run_linux_auto_scope_test() {
    local distro_id="$1"
    local distro_like="$2"
    local expected_primary_search="$3"
    local cache_root="${TMP_DIR}/cache-root-linux-scope-${distro_id}"

    reset_log
    rm -rf "${cache_root}"
    local os_file
    os_file="${TMP_DIR}/os-release-linux-scope-${distro_id}"
    {
        printf "ID=%s\n" "${distro_id}"
        if [[ -n "${distro_like}" ]]; then
            printf "ID_LIKE=\"%s\"\n" "${distro_like}"
        fi
    } >"${os_file}"

    export FPF_TEST_UNAME="Linux"
    printf "y\n" | FPF_CACHE_DIR="${cache_root}" FPF_OS_RELEASE_FILE="${os_file}" "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "${expected_primary_search}"
    assert_contains "snap find sample-query"
    assert_contains "flatpak search"
    assert_not_contains "npm search sample-query"
}

run_auto_detect_update_test() {
    local distro_id="$1"
    local distro_like="$2"
    local expected="$3"

    reset_log
    local os_file
    os_file="${TMP_DIR}/os-release-${distro_id}"
    {
        printf "ID=%s\n" "${distro_id}"
        if [[ -n "${distro_like}" ]]; then
            printf "ID_LIKE=\"%s\"\n" "${distro_like}"
        fi
    } >"${os_file}"

    export FPF_TEST_UNAME="Linux"
    printf "y\n" | FPF_OS_RELEASE_FILE="${os_file}" "${FPF_BIN}" -U >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "${expected}"
    assert_contains "snap refresh"
    assert_contains "flatpak update -y --user"
    assert_contains "brew update"
    assert_contains "bun update"
    assert_not_contains "npm update -g"
}

run_preview_cache_reuse_test() {
    local preview_tmpdir="${TMP_DIR}/preview-cache-reuse"
    local apt_show_count=0
    local dpkg_list_count=0

    reset_log
    rm -rf "${preview_tmpdir}"
    mkdir -p "${preview_tmpdir}"

    printf "n\n" | TMPDIR="${preview_tmpdir}" FPF_TEST_FZF_PREVIEW_REPEAT=2 "${FPF_BIN}" --manager apt sample-query >/dev/null

    apt_show_count="$(grep -c '^apt-cache show ' "${LOG_FILE}" || true)"
    dpkg_list_count="$(grep -c '^dpkg -L ' "${LOG_FILE}" || true)"
    if [[ "${apt_show_count}" -ne 1 || "${dpkg_list_count}" -ne 1 ]]; then
        printf "Expected repeated preview invocations to reuse one cached result (apt-cache show=%s dpkg -L=%s)\n" "${apt_show_count}" "${dpkg_list_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
}

run_preview_cache_cleanup_test() {
    local preview_tmpdir="${TMP_DIR}/preview-cache-cleanup"

    reset_log
    rm -rf "${preview_tmpdir}"
    mkdir -p "${preview_tmpdir}"

    printf "n\n" | TMPDIR="${preview_tmpdir}" FPF_TEST_FZF_PREVIEW_REPEAT=2 "${FPF_BIN}" --manager apt sample-query >/dev/null

    if compgen -G "${preview_tmpdir}/fpf/session.*" >/dev/null; then
        printf "Expected session temp root to be cleaned after exit, but found:\n" >&2
        printf "%s\n" "$(ls -1 "${preview_tmpdir}/fpf")" >&2
        exit 1
    fi
}

run_search_install_test apt "apt-get install -y"
run_remove_test apt "apt-get remove -y"
run_list_test apt "apt-cache show"
run_update_test apt "apt-get update"

run_search_install_test dnf "dnf install -y"
run_remove_test dnf "dnf remove -y"
run_list_test dnf "dnf info"
run_update_test dnf "dnf upgrade -y"

run_search_install_test pacman "pacman -S --needed"
run_remove_test pacman "pacman -Rsn"
run_list_test pacman "pacman -Qi"
run_update_test pacman "pacman -Syu"

run_search_install_test zypper "zypper --non-interactive install --auto-agree-with-licenses"
run_remove_test zypper "zypper --non-interactive remove"
run_list_test zypper "zypper --non-interactive info"
run_update_test zypper "zypper --non-interactive refresh"

run_search_install_test emerge "emerge --ask=n --verbose"
run_remove_test emerge "emerge --ask=n --deselect"
run_list_test emerge "emerge --search --color=n"
run_update_test emerge "emerge --sync"

run_search_install_test brew "brew install"
run_remove_test brew "brew uninstall"
run_list_test brew "brew info"
run_update_test brew "brew update"

run_search_install_test winget "winget install --id"
run_remove_test winget "winget uninstall --id"
run_list_test winget "winget show --id"
run_update_test winget "winget upgrade --all"

run_search_install_test choco "choco install"
run_remove_test choco "choco uninstall"
run_list_test choco "choco info"
run_update_test choco "choco upgrade all -y"

run_search_install_test scoop "scoop install"
run_remove_test scoop "scoop uninstall"
run_list_test scoop "scoop info"
run_update_test scoop "scoop update"

run_search_install_test snap "snap install"
run_remove_test snap "snap remove"
run_list_test snap "snap info"
run_update_test snap "snap refresh"

run_search_install_test flatpak "flatpak install -y --user flathub"
run_remove_test flatpak "flatpak uninstall -y --user"
run_list_test flatpak "flatpak info"
run_update_test flatpak "flatpak update -y --user"

run_search_install_test npm "npm install -g"
run_remove_test npm "npm uninstall -g"
run_list_test npm "npm view"
run_update_test npm "npm update -g"

run_search_install_test bun "bun add -g"
run_remove_test bun "bun remove --global"
run_list_test bun "bun info"
run_update_test bun "bun update"

run_fzf_bootstrap_test

reset_log
run_auto_detect_update_test ubuntu debian "apt-get update"
run_auto_detect_update_test fedora "rhel fedora" "dnf upgrade -y"
run_auto_detect_update_test arch arch "pacman -Syu"
run_auto_detect_update_test opensuse-tumbleweed "suse opensuse" "zypper --non-interactive refresh"
run_auto_detect_update_test gentoo gentoo "emerge --sync"
run_linux_auto_scope_test ubuntu debian "apt-cache dumpavail"

run_macos_auto_scope_test
run_macos_auto_update_test
run_macos_no_query_scope_test
run_dynamic_reload_default_auto_test "Darwin"
run_dynamic_reload_default_auto_test "Linux"
run_dynamic_reload_default_auto_test "MINGW64_NT-10.0"
run_dynamic_reload_no_listen_fallback_test
run_dynamic_reload_single_mode_single_manager_test "brew"
run_dynamic_reload_single_mode_single_manager_test "bun"
run_dynamic_reload_single_mode_multi_manager_test "Linux"
run_dynamic_reload_single_mode_multi_manager_test "Darwin"
run_dynamic_reload_force_never_test "Darwin"
run_dynamic_reload_force_never_test "Linux"
run_dynamic_reload_force_never_test "MINGW64_NT-10.0"
run_dynamic_reload_override_test "apt"
run_dynamic_reload_override_test "brew"
run_dynamic_reload_override_test "bun"
run_dynamic_reload_override_test "npm"
run_dynamic_reload_override_test "winget"
run_fzf_ui_regression_guard_test
run_installed_cache_test
run_apt_catalog_cache_rebuild_test
run_brew_catalog_cache_rebuild_test
run_query_cache_layout_test
run_winget_id_parsing_test
run_bun_global_scope_guard_test
run_bun_search_single_call_test
run_bun_query_cache_warm_immediate_test
run_bun_refresh_failure_fallback_test
run_bun_ttl_expiration_schedules_refresh_test
run_bun_corrupt_cache_fallback_test
run_bun_generation_ordering_test
run_dynamic_reload_override_auto_parity_test
run_bun_tree_installed_remove_test
run_bun_scoped_tree_remove_test
run_npm_scoped_remove_test
run_npm_scoped_remove_windows_path_test
run_bun_fallback_npm_scoped_remove_test
run_flatpak_scope_fallback_test
run_flatpak_no_query_catalog_test
run_flatpak_no_remote_config_error_test
run_winget_no_source_config_error_test
run_choco_no_source_config_error_test
run_scoop_no_bucket_config_error_test
run_windows_auto_scope_test
run_windows_auto_update_test
run_exact_lookup_recovery_test
run_bun_exact_lookup_without_npm_view_test
run_multi_token_exact_priority_test
run_all_manager_default_scope_test "Darwin"
run_all_manager_default_scope_test "Linux"
run_all_manager_default_scope_test "MINGW64_NT-10.0"
run_feed_search_manager_mix_test "Darwin"
run_feed_search_manager_mix_test "Linux"
run_feed_search_manager_mix_test "MINGW64_NT-10.0"
run_fixture_catalog_feed_search_test
run_feed_search_merged_tsv_contract_test
run_feed_search_installed_marker_path_test
run_feed_search_skip_marker_warm_path_test
run_fzf_listen_help_simulation_test
run_cache_refresh_signal_simulation_test
run_preview_cache_reuse_test
run_preview_cache_cleanup_test

reset_log
export FPF_TEST_UNAME="Linux"
UNQUOTED_OS_FILE="${TMP_DIR}/os-release-unquoted"
cat >"${UNQUOTED_OS_FILE}" <<'EOF'
ID=fedora
ID_LIKE=rhel fedora
EOF
printf "y\n" | FPF_OS_RELEASE_FILE="${UNQUOTED_OS_FILE}" "${FPF_BIN}" -U >/dev/null
unset FPF_TEST_UNAME
assert_contains "dnf upgrade -y"
assert_contains "bun update"

reset_log
"${FPF_BIN}" -h >/dev/null

reset_log
version_output="$("${FPF_BIN}" -v)"
expected_version="$(awk -F'"' '/"version":/ {print $4; exit}' "${ROOT_DIR}/package.json")"
if [[ -z "${expected_version}" ]]; then
    printf "Unable to read expected version from package.json\n" >&2
    exit 1
fi
if [[ "${version_output}" != "fpf ${expected_version}" ]]; then
    printf "Expected version output 'fpf %s', got: %s\n" "${expected_version}" "${version_output}" >&2
    exit 1
fi
if [[ -s "${LOG_FILE}" ]]; then
    printf "Expected version command to avoid manager calls, but log is not empty:\n%s\n" "$(cat "${LOG_FILE}")" >&2
    exit 1
fi

printf "All smoke tests passed.\n"
