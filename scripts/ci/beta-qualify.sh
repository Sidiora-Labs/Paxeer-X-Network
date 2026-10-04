#!/usr/bin/env bash
set -euo pipefail
umask 077

beta_qualify_focused() {
    local root spec='' ledger='' directory=${PAXEER_X_EVIDENCE_DIR:-} task='' timeout=600 kind=file
    root=$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)
    while [ "$#" -gt 0 ]; do
        case $1 in
        --spec|--ledger|--evidence-root|--task|--timeout|--evidence-kind)
            [ "$#" -ge 2 ] || return 2
            case $1 in
            --spec) spec=$2 ;; --ledger) ledger=$2 ;; --evidence-root) directory=$2 ;;
            --task) task=$2 ;; --timeout) timeout=$2 ;; --evidence-kind) kind=$2 ;;
            esac
            shift 2 ;;
        --) shift; break ;;
        -h|--help)
            echo 'usage: beta-qualify.sh --spec PATH --task ID --ledger PRIVATE_PATH --evidence-root PRIVATE_DIR [--timeout SECONDS] -- "make TARGET"'
            return 0 ;;
        *) break ;;
        esac
    done
    [ -n "$spec" ] && [ -n "$ledger" ] && [ -n "$directory" ] && [ -n "$task" ] && [ "$#" -gt 0 ] || {
        echo 'beta-qualify: explicit spec, task, private ledger and evidence root required' >&2
        return 2
    }
    exec bash "$root/scripts/ci/beta-ledger-check.sh" --source-run --root "$root" \
        --evidence-root "$directory" --spec "$spec" --task "$task" --ledger "$ledger" \
        --timeout "$timeout" --evidence-kind "$kind" -- "$@"
}

beta_qualify_focused "$@"
