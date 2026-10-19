#!/bin/sh
set -eu

scan() {
    scan_list=$(mktemp)
    scan_hits=$(mktemp)
    cat > "$scan_list"
    report() {
        while IFS= read -r scan_hit; do
            scan_file=${scan_hit%%:*}
            scan_rest=${scan_hit#*:}
            scan_line=${scan_rest%%:*}
            scan_match=${scan_rest#*:}
            printf '%s:%s: %s: %.12s\n' "$scan_file" "$scan_line" "$1" "$scan_match"
        done
    }
    run_rg() {
        xargs -0 -r -n 200 rg --hidden --no-heading --with-filename -n -o "$@" -- < "$scan_list" || true
    }
    run_rg '(^|[^A-Za-z0-9+/])AKIA[0-9A-Z]{16}($|[^A-Za-z0-9+/])' \
        | sed 's/^\([^:]*:[0-9]*:\).\{0,1\}AKIA/\1AKIA/' | report aws-access-key >> "$scan_hits"
    run_rg 'gh[pousr]_[A-Za-z0-9]{36,}' | report github-token >> "$scan_hits"
    run_rg 'xox[baprs]-[A-Za-z0-9-]{20,}' | report slack-token >> "$scan_hits"
    run_rg 'sk_live_[A-Za-z0-9]{20,}' | report stripe-live-key >> "$scan_hits"
    xargs -0 -r -n 200 rg --hidden -U -l -- \
        '-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----(\\[nr]|[\r\n \t"'"'"'+`]|(Proc-Type|DEK-Info|Comment): [^\r\n\\]*)+[A-Za-z0-9+/]{40,}' \
        < "$scan_list" 2>/dev/null \
        | while IFS= read -r pem_file; do
            rg --no-heading --with-filename -n -o -m 1 -- '-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----' "$pem_file"
        done | report pem-private-key >> "$scan_hits" || true
    run_rg '(^|[^A-Za-z0-9_-])eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}' \
        | while IFS= read -r jwt_hit; do
            jwt_token=${jwt_hit#*:}
            jwt_token=${jwt_token#*:}
            jwt_token=${jwt_token#"${jwt_token%%eyJ*}"}
            jwt_header=$(printf '%s' "${jwt_token%%.*}" | tr '_-' '/+')
            while [ $((${#jwt_header} % 4)) -ne 0 ]; do
                jwt_header="$jwt_header="
            done
            jwt_json=$(printf '%s' "$jwt_header" | base64 -d 2>/dev/null) || continue
            case "$jwt_json" in
                '{'*'}') printf '%s:%s\n' "${jwt_hit%%:*}" "$(printf '%s' "${jwt_hit#*:}" | cut -d: -f1):$jwt_token" ;;
            esac
        done | report jwt >> "$scan_hits"
    if [ -s "$scan_hits" ]; then
        cat "$scan_hits"
        rm -f "$scan_list" "$scan_hits"
        return 1
    fi
    rm -f "$scan_list" "$scan_hits"
}

self_test() {
    fixture=$(mktemp -d)
    pem_begin='-----BEGIN'
    pem_body=$(printf 'selftest-key-material-%s' "$(seq -s - 1 12)" | base64 | tr -d '\n=' | cut -c1-64)
    b64url() { printf '%s' "$1" | base64 | tr -d '\n=' | tr '+/' '-_'; }
    jwt="$(b64url '{"alg":"HS256","typ":"JWT"}').$(b64url '{"sub":"secret-scan-self-test","iat":1}').$(b64url 'self-test-signature-bytes-000')"
    printf 'token = "%s"\n' "$jwt" > "$fixture/pos-jwt.txt"
    printf '%s PRIVATE KEY-----\n%s\n%s\n-----END PRIVATE KEY-----\n' "$pem_begin" "$pem_body" "$pem_body" > "$fixture/pos-pem.pem"
    printf 'key := "%s EC PRIVATE KEY-----\\n%s\\n-----END EC PRIVATE KEY-----"\n' "$pem_begin" "$pem_body" > "$fixture/pos-pem-literal.go"
    printf 'id = "AK%s"\n' 'IAIOSFODNN7EXAMPLE' > "$fixture/pos-aws.txt"
    printf '<svg><image href="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAKIAQUFxg0toWx%sAkiaRKPBjC6jEYJAKIAABCDEFGHIJKLMNOPqrstuvwxyzeyJ%s"/></svg>\n' \
        "$pem_body" "$pem_body" > "$fixture/neg-data-uri.svg"
    printf 'let pem = String::from("%s PRIVATE KEY-----\\n"));\nfor line in encoded.as_bytes().chunks(64) {\n' "$pem_begin" > "$fixture/neg-pem-header.rs"
    printf "for raw in [b'%s PRIVATE KEY-----']:\n" "$pem_begin" > "$fixture/neg-pem-header.py"
    printf 'PrivateTLSKeyPEM: "%s PRIVATE KEY-----",\n\t\tSocialCursorKey: strings.Repeat("A", 43), Name: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP"\n' "$pem_begin" > "$fixture/neg-pem-header.go"
    self_test_failed=0
    for case_file in "$fixture"/*; do
        if printf '%s\0' "$case_file" | scan > /dev/null; then case_hit=0; else case_hit=1; fi
        case "${case_file##*/}:$case_hit" in
            pos-*:1 | neg-*:0) ;;
            *)
                echo "secret scan self-test failed: ${case_file##*/} (hit=$case_hit)" >&2
                self_test_failed=1
                ;;
        esac
    done
    rm -rf "$fixture"
    return "$self_test_failed"
}

case "${1:-}" in
    --self-test) self_test ;;
    '') scan ;;
    *)
        echo "usage: secret-scan.sh [--self-test] < NUL-separated file list" >&2
        exit 2
        ;;
esac
