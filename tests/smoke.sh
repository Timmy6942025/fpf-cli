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
                printf "brewpkg\n"
                ;;
            list)
                if [[ "${2:-}" == "--versions" ]]; then
                    printf "brewpkg 1.0\n"
                fi
                ;;
            info)
                printf "brewpkg: stable 1.0\n"
                ;;
        esac
        ;;
    winget)
        case "${1:-}" in
            search)
                printf "Name Id Version Source\n"
                printf '%s\n' '--------------------------------'
                printf "WingetPkg winget.pkg 1.0 winget\n"
                ;;
            list)
                printf "Name Id Version Source\n"
                printf '%s\n' '--------------------------------'
                printf "WingetPkg winget.pkg 1.0 winget\n"
                ;;
            show)
                printf "Found package: winget.pkg\n"
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
        esac
        ;;
    npm)
        case "${1:-}" in
            search)
                printf "npmpkg\tNpm package\n"
                ;;
            ls)
                printf "/usr/lib/node_modules\n"
                printf "/usr/lib/node_modules/npmpkg\n"
                ;;
            view)
                printf "name = 'npmpkg'\n"
                ;;
        esac
        ;;
    bun)
        case "${1:-}" in
            search)
                printf "Name Description\n"
                printf "bunpkg Bun package\n"
                ;;
            info)
                printf "name: bunpkg\n"
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
}

run_macos_no_query_scope_test() {
    reset_log
    export FPF_TEST_UNAME="Darwin"
    printf "n\n" | "${FPF_BIN}" >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "brew list --versions"
    assert_contains "bun pm ls --global"
    assert_not_contains "brew search"
}

run_windows_auto_scope_test() {
    reset_log
    export FPF_TEST_UNAME="MINGW64_NT-10.0"
    printf "y\n" | "${FPF_BIN}" sample-query >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "winget search sample-query --source winget"
    assert_contains "choco search sample-query --limit-output"
    assert_contains "scoop search sample-query"
    assert_contains "npm search sample-query"
    assert_contains "bun search sample-query"
    assert_not_contains "apt-cache search"
}

run_windows_auto_update_test() {
    reset_log
    export FPF_TEST_UNAME="MINGW64_NT-10.0"
    printf "y\n" | "${FPF_BIN}" -U >/dev/null
    unset FPF_TEST_UNAME

    assert_contains "winget upgrade --all --source winget"
    assert_contains "choco upgrade all -y"
    assert_contains "scoop update"
    assert_contains "npm update -g"
    assert_contains "bun update"
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
    assert_contains "npm search sample-query"
    assert_contains "bun search sample-query"
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
    assert_contains "npm update -g"
    assert_contains "bun update"
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
run_windows_auto_scope_test
run_windows_auto_update_test

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
assert_contains "npm update -g"

reset_log
"${FPF_BIN}" -h >/dev/null

printf "All smoke tests passed.\n"
