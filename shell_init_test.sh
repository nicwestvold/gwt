#!/usr/bin/env bash
# Integration tests for the gwt shell wrapper function.
# Usage: bash shell_init_test.sh

set -euo pipefail

RESULTS_FILE=$(mktemp)
echo "0 0" > "$RESULTS_FILE"

assert_eq() {
    local label="$1" got="$2" want="$3"
    local pass fail
    read -r pass fail < "$RESULTS_FILE"
    if [ "$got" = "$want" ]; then
        echo "  PASS: $label"
        echo "$((pass + 1)) $fail" > "$RESULTS_FILE"
    else
        echo "  FAIL: $label (got '$got', want '$want')"
        echo "$pass $((fail + 1))" > "$RESULTS_FILE"
    fi
}

# Mock gwt binary: writes $MOCK_CD_PATH to GWT_CD_FILE and exits with $MOCK_EXIT.
mock_gwt() {
    local bin
    bin=$(mktemp)
    cat > "$bin" <<'SCRIPT'
#!/usr/bin/env bash
if [ -n "${GWT_CD_FILE:-}" ] && [ -n "${MOCK_CD_PATH:-}" ]; then
    printf '%s' "$MOCK_CD_PATH" > "$GWT_CD_FILE"
fi
exit "${MOCK_EXIT:-0}"
SCRIPT
    chmod +x "$bin"
    echo "$bin"
}

# Source the wrapper function, but replace "command gwt" with our mock.
load_wrapper() {
    local mock_bin="$1"
    eval "$(cat <<WRAPPER
gwt() {
    if [ "\${1}" = "add" ] || [ "\${1}" = "clone" ]; then
        local _gwt_cd_file
        _gwt_cd_file=\$(mktemp)
        GWT_CD_FILE="\$_gwt_cd_file" "$mock_bin" "\$@"
        local _gwt_exit=\$?
        if [ -s "\$_gwt_cd_file" ]; then
            builtin cd "\$(cat "\$_gwt_cd_file")" || true
        fi
        rm -f "\$_gwt_cd_file"
        return \$_gwt_exit
    else
        "$mock_bin" "\$@"
    fi
}
WRAPPER
)"
}

# --- Test: gwt add writes cd file and wrapper changes directory ---
echo "Test: gwt add auto-cd"
(
    mock_bin=$(mock_gwt)
    target=$(mktemp -d)
    export MOCK_CD_PATH="$target"
    export MOCK_EXIT=0
    load_wrapper "$mock_bin"

    gwt add my-feature
    assert_eq "cwd changed to target" "$(pwd)" "$target"

    rm -f "$mock_bin"
    rm -rf "$target"
)

# --- Test: gwt clone writes cd file and wrapper changes directory ---
echo "Test: gwt clone auto-cd"
(
    mock_bin=$(mock_gwt)
    target=$(mktemp -d)
    export MOCK_CD_PATH="$target"
    export MOCK_EXIT=0
    load_wrapper "$mock_bin"

    gwt clone https://example.com/repo.git
    assert_eq "cwd changed to target" "$(pwd)" "$target"

    rm -f "$mock_bin"
    rm -rf "$target"
)

# --- Test: gwt list (passthrough) does not attempt cd ---
echo "Test: gwt list passthrough"
(
    mock_bin=$(mock_gwt)
    export MOCK_CD_PATH="/nonexistent"
    export MOCK_EXIT=0
    load_wrapper "$mock_bin"

    start_dir=$(pwd)
    gwt list
    assert_eq "cwd unchanged" "$(pwd)" "$start_dir"

    rm -f "$mock_bin"
)

# --- Test: non-zero exit code preserved ---
echo "Test: non-zero exit preserved"
(
    mock_bin=$(mock_gwt)
    target=$(mktemp -d)
    export MOCK_CD_PATH="$target"
    export MOCK_EXIT=1
    load_wrapper "$mock_bin"

    set +e
    gwt add failing-branch
    exit_code=$?
    set -e
    assert_eq "exit code is 1" "$exit_code" "1"

    rm -f "$mock_bin"
    rm -rf "$target"
)

# --- Test: empty cd file is ignored ---
echo "Test: empty cd file ignored"
(
    mock_bin=$(mock_gwt)
    export MOCK_CD_PATH=""
    export MOCK_EXIT=0
    load_wrapper "$mock_bin"

    start_dir=$(pwd)
    gwt add my-feature
    assert_eq "cwd unchanged with empty cd file" "$(pwd)" "$start_dir"

    rm -f "$mock_bin"
)

# --- Summary ---
echo ""
read -r pass fail < "$RESULTS_FILE"
rm -f "$RESULTS_FILE"
echo "Results: $pass passed, $fail failed"
if [ "$fail" -gt 0 ]; then
    exit 1
fi
