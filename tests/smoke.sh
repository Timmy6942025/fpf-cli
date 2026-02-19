#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FPF_BIN="${ROOT_DIR}/fpf"

TMP_DIR="$(mktemp -d)"
MOCK_BIN="${TMP_DIR}/mock-bin"
LOG_FILE="${TMP_DIR}/calls.log"

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
        awk 'NR == 1 { print; exit }'
        ;;
    apt-cache)
        case "${1:-}" in
            search)
                printf "aptpkg - Apt package\n"
                ;;
            show)
                printf "Package: %s\nVersion: 1.0\n" "${2:-aptpkg}"
                ;;
        esac
        ;;
    dpkg-query)
        printf "aptpkg\t1.0\n"
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
            search)
                if [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" && "${2:-}" == "opencode" ]]; then
                    printf "opencode-tools\n"
                else
                    printf "brewpkg\n"
                fi
                ;;
            list)
                if [[ "${2:-}" == "--versions" ]]; then
                    printf "brewpkg 1.0\n"
                fi
                ;;
            info)
                if [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" ]]; then
                    pkg="${!#}"
                    if [[ "${pkg}" == "opencode" ]]; then
                        printf "opencode: stable 1.0\n"
                    else
                        exit 1
                    fi
                else
                    printf "brewpkg: stable 1.0\n"
                fi
                ;;
        esac
        ;;
    winget)
        case "${1:-}" in
            search)
                printf "Name Id Version Source\n"
                printf '%s\n' '--------------------------------'
                printf "Visual Studio Code  Microsoft.VisualStudioCode  1.0  winget\n"
                ;;
            list)
                printf "Name Id Version Source\n"
                printf '%s\n' '--------------------------------'
                printf "Visual Studio Code  Microsoft.VisualStudioCode  1.0  winget\n"
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
            search)
                printf "chocopkg|1.0\n"
                ;;
            list)
                printf "chocopkg|1.0\n"
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
            search)
                printf "Name Version Source Updated Info\n"
                printf "scooppkg 1.0 main now Scoop package\n"
                ;;
            list)
                printf "Name Version Source Updated Info\n"
                printf "scooppkg 1.0 main now installed\n"
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
            remote-ls)
                printf "Application Description\n"
                printf "org.example.Flat Flatpak package\n"
                ;;
            search)
                printf "Application Description\n"
                printf "org.example.Flat Flatpak package\n"
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
                printf "/usr/lib/node_modules\n"
                printf "/usr/lib/node_modules/npmpkg\n"
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
                printf "Name Description\n"
                if [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" && "${2:-}" == "opencode" ]]; then
                    printf "opencode-tools Bun helper package\n"
                else
                    printf "bunpkg Bun package\n"
                fi
                ;;
            info)
                if [[ "${FPF_TEST_EXACT_LOOKUP:-0}" == "1" ]]; then
                    if [[ "${2:-}" == "opencode" ]]; then
                        printf "name: opencode\n"
                    else
                        exit 1
                    fi
                else
                    printf "name: bunpkg\n"
                fi
                ;;
            pm)
                if [[ "${2:-}" == "ls" ]]; then
                    printf "Name Version\n"
                    printf "bunpkg 1.0\n"
                fi
                ;;
            add|remove|update)
                ;;
        esac
        ;;
esac
EOF

chmod +x "${MOCK_BIN}/mockcmd"

for cmd in uname sudo fzf apt-cache dpkg-query dpkg apt-get dnf rpm pacman zypper emerge qlist brew winget choco scoop snap flatpak npm bun; do
    ln -s "${MOCK_BIN}/mockcmd" "${MOCK_BIN}/${cmd}"
done

export PATH="${MOCK_BIN}:/usr/bin:/bin"
export FPF_TEST_LOG="${LOG_FILE}"
export FPF_TEST_MOCK_BIN="${MOCK_BIN}"
export FPF_TEST_MOCKCMD_PATH="${MOCK_BIN}/mockcmd"

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
    printf "y\n" | "${FPF_BIN}" --manager apt sample-query >/dev/null
    unset FPF_TEST_BOOTSTRAP_FZF
    ln -sf "${MOCK_BIN}/mockcmd" "${MOCK_BIN}/fzf"

    assert_contains "apt-get install -y fzf"
    assert_contains "apt-get install -y aptpkg"
}

run_macos_auto_scope_test() {
    reset_log
    export FPF_TEST_UNAME="Darwin"
    printf "y\n" | "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "brew search sample-query"
    assert_contains "bun search sample-query"
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

    assert_contains "brew search aa"
    assert_contains "bun search aa"
    assert_not_contains "npm search aa"
}

run_dynamic_reload_default_auto_test() {
    local uname_value="$1"

    reset_log
    export FPF_TEST_UNAME="${uname_value}"
    printf "n\n" | "${FPF_BIN}" >/dev/null
    unset FPF_TEST_UNAME

    assert_not_contains "--bind=start:reload:"
    assert_contains "--bind=change:reload:"
    assert_contains "FPF_SKIP_INSTALLED_MARKERS=1"
    assert_contains "--feed-search -- \"\$q\""
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
    assert_contains "--bind=change:reload:"
    assert_contains "--feed-search --manager ${manager} -- \"\$q\""
}

run_installed_cache_test() {
    reset_log
    rm -rf "${TMP_DIR}/fpf"

    TMPDIR="${TMP_DIR}" "${FPF_BIN}" --manager brew --feed-search -- sample-query >/dev/null
    TMPDIR="${TMP_DIR}" "${FPF_BIN}" --manager brew --feed-search -- other-query >/dev/null

    local brew_list_count
    brew_list_count="$(grep -c '^brew list --versions$' "${LOG_FILE}" || true)"
    if [[ "${brew_list_count}" -ne 1 ]]; then
        printf "Expected brew installed list to be cached (1 call), got %s\n" "${brew_list_count}" >&2
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
    if [[ "${bun_search_count}" -ne 1 ]]; then
        printf "Expected one bun search call, got %s\n" "${bun_search_count}" >&2
        printf "Actual log:\n%s\n" "$(cat "${LOG_FILE}")" >&2
        exit 1
    fi
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

run_windows_auto_scope_test() {
    reset_log
    export FPF_TEST_UNAME="MINGW64_NT-10.0"
    printf "y\n" | "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "winget search sample-query --source winget"
    assert_contains "choco search sample-query --limit-output"
    assert_contains "scoop search sample-query"
    assert_contains "bun search sample-query"
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
    assert_contains "brew search opencode"
    assert_contains "brew info --formula opencode"
    assert_contains "brew install opencode"

    reset_log
    printf "y\n" | "${FPF_BIN}" --manager npm opencode >/dev/null
    assert_contains "npm search opencode --searchlimit"
    assert_contains "npm view opencode name"
    assert_contains "npm install -g opencode"

    reset_log
    export FPF_TEST_BUN_SEARCH_FAIL="1"
    printf "y\n" | "${FPF_BIN}" --manager bun opencode >/dev/null
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
    printf "y\n" | "${FPF_BIN}" --manager bun opencode >/dev/null
    unset FPF_TEST_NPM_VIEW_FAIL
    unset FPF_TEST_EXACT_LOOKUP

    assert_contains "bun search opencode"
    assert_contains "bun info opencode"
    assert_logged_exact "bun add -g opencode"
}

run_all_manager_default_scope_test() {
    local uname_value="$1"

    reset_log
    export FPF_TEST_UNAME="${uname_value}"
    printf "n\n" | "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "apt-cache search -- sample-query"
    assert_contains "dnf -q list available"
    assert_contains "pacman -Ss -- sample-query"
    assert_contains "zypper --non-interactive --quiet search --details --type package sample-query"
    assert_contains "emerge --searchdesc --color=n sample-query"
    assert_contains "brew search sample-query"
    assert_contains "winget search sample-query --source winget"
    assert_contains "choco search sample-query --limit-output"
    assert_contains "scoop search sample-query"
    assert_contains "snap find sample-query"
    assert_contains "flatpak search --columns=application,description sample-query"
    assert_contains "bun search sample-query"
    assert_not_contains "npm search sample-query --searchlimit"
}

run_linux_auto_scope_test() {
    local distro_id="$1"
    local distro_like="$2"
    local expected_primary_search="$3"

    reset_log
    local os_file
    os_file="${TMP_DIR}/os-release-linux-scope-${distro_id}"
    {
        printf "ID=%s\n" "${distro_id}"
        if [[ -n "${distro_like}" ]]; then
            printf "ID_LIKE=\"%s\"\n" "${distro_like}"
        fi
    } >"${os_file}"

    export FPF_TEST_UNAME="Linux"
    printf "y\n" | FPF_OS_RELEASE_FILE="${os_file}" "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "${expected_primary_search}"
    assert_contains "snap find sample-query"
    assert_contains "flatpak search"
    assert_contains "brew search sample-query"
    assert_contains "bun search sample-query"
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
run_linux_auto_scope_test ubuntu debian "apt-cache search -- sample-query"

run_macos_auto_scope_test
run_macos_auto_update_test
run_macos_no_query_scope_test
run_dynamic_reload_default_auto_test "Darwin"
run_dynamic_reload_default_auto_test "Linux"
run_dynamic_reload_default_auto_test "MINGW64_NT-10.0"
run_dynamic_reload_force_never_test "Darwin"
run_dynamic_reload_force_never_test "Linux"
run_dynamic_reload_force_never_test "MINGW64_NT-10.0"
run_dynamic_reload_override_test "brew"
run_dynamic_reload_override_test "npm"
run_dynamic_reload_override_test "winget"
run_installed_cache_test
run_winget_id_parsing_test
run_bun_global_scope_guard_test
run_bun_search_single_call_test
run_flatpak_scope_fallback_test
run_flatpak_no_query_catalog_test
run_windows_auto_scope_test
run_windows_auto_update_test
run_exact_lookup_recovery_test
run_bun_exact_lookup_without_npm_view_test
run_all_manager_default_scope_test "Darwin"
run_all_manager_default_scope_test "Linux"
run_all_manager_default_scope_test "MINGW64_NT-10.0"

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
