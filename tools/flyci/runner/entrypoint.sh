#!/bin/sh
set -eu

runner_user=runner
runner_home=/home/runner
runner_dir=$runner_home/actions-runner
job_marker='Running job:'

log() {
    printf 'entrypoint: %s\n' "$*" >&2
}

as_runner() {
    env HOME="$runner_home" USER="$runner_user" LOGNAME="$runner_user" \
        setpriv --reuid="$runner_user" --regid="$runner_user" --init-groups -- "$@"
}

selfcheck() {
    if ! runner_version=$(as_runner "$runner_dir/bin/Runner.Listener" --version); then
        log "selfcheck: the runner listener did not report its version"
        return 1
    fi
    printf 'runner %s\n' "$runner_version"

    for tool in \
        bash bc clang clang++ cmake curl dbus-daemon file g++ gcc git git-lfs \
        gnome-keyring-daemon gpg jq ld.lld llvm-ar llvm-config make ninja \
        openssl pip3 pkg-config protoc python3 redis-server rg rsync socat \
        sudo tar unzip xz zip zstd certutil authbind \
        rustup cargo rustc; do
        if ! tool_path=$(as_runner which "$tool"); then
            log "selfcheck: $tool is not on the runner user's PATH"
            return 1
        fi
        printf 'ok %s %s\n' "$tool" "$tool_path"
    done

    if ! as_runner sudo --non-interactive true; then
        log "selfcheck: sudo asks the runner user for a password"
        return 1
    fi
    printf 'ok passwordless sudo\n'

    if ! as_runner rustup target list --installed | grep -qx wasm32-unknown-unknown; then
        log "selfcheck: the wasm32-unknown-unknown target is not installed"
        return 1
    fi
    printf 'ok wasm32-unknown-unknown\n'

    if ! as_runner python3 -c 'import venv, yaml, pytest'; then
        log "selfcheck: python3 lacks venv, yaml or pytest"
        return 1
    fi
    printf 'ok python3 venv yaml pytest\n'

    if ! as_runner pkg-config --exists openssl sqlite3 zlib libzstd; then
        log "selfcheck: pkg-config cannot find openssl, sqlite3, zlib or libzstd"
        return 1
    fi
    printf 'ok pkg-config openssl sqlite3 zlib libzstd\n'

    tool_cache=${RUNNER_TOOL_CACHE:-}
    if [ -z "$tool_cache" ] || ! as_runner test -d "$tool_cache" || ! as_runner test -w "$tool_cache"; then
        log "selfcheck: RUNNER_TOOL_CACHE is unset or not writable by the runner user"
        return 1
    fi
    printf 'ok RUNNER_TOOL_CACHE %s\n' "$tool_cache"

    printf 'selfcheck passed\n'
}

run_job() {
    jitconfig=${RUNNER_JITCONFIG:-}
    if [ -z "$jitconfig" ]; then
        log "RUNNER_JITCONFIG is not set; refusing to start a runner without its just-in-time configuration"
        return 64
    fi
    unset RUNNER_JITCONFIG

    idle_timeout=${RUNNER_IDLE_TIMEOUT:-900}
    case $idle_timeout in
        '' | *[!0-9]*)
            log "RUNNER_IDLE_TIMEOUT must be a whole number of seconds, got '$idle_timeout'"
            return 64
            ;;
    esac

    state_dir=$(mktemp -d)
    output_fifo=$state_dir/output
    started_marker=$state_dir/started
    mkfifo "$output_fifo"

    cd "$runner_dir"
    as_runner setsid ./run.sh --jitconfig "$jitconfig" > "$output_fifo" 2>&1 &
    runner_pid=$!
    unset jitconfig

    trap 'kill -TERM -- "-$runner_pid" 2>/dev/null || true' INT TERM

    (
        sleep "$idle_timeout"
        if [ ! -e "$started_marker" ]; then
            log "no job started within $idle_timeout seconds; stopping the runner"
            kill -TERM -- "-$runner_pid" 2>/dev/null || true
            sleep 30
            kill -KILL -- "-$runner_pid" 2>/dev/null || true
        fi
    ) &
    watchdog_pid=$!

    while IFS= read -r line; do
        printf '%s\n' "$line"
        case $line in
            *"$job_marker"*)
                if [ ! -e "$started_marker" ]; then
                    : > "$started_marker"
                    log "job started; idle watchdog disarmed"
                fi
                ;;
        esac
    done < "$output_fifo"

    runner_status=0
    wait "$runner_pid" || runner_status=$?
    kill "$watchdog_pid" 2>/dev/null || true

    if [ ! -e "$started_marker" ]; then
        log "the runner exited without running a job"
    fi
    rm -rf "$state_dir"
    log "runner exited with status $runner_status"
    return "$runner_status"
}

if [ "$(id -u)" -ne 0 ]; then
    log "must start as root so it can drop to the $runner_user user"
    exit 64
fi

case ${1:-} in
    --selfcheck)
        selfcheck
        ;;
    '')
        run_job
        ;;
    *)
        log "unknown argument '$1'; expected no argument or --selfcheck"
        exit 64
        ;;
esac
