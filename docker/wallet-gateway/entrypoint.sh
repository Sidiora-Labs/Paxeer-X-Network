#!/bin/sh
set -eu

: "${WALLET_IDENTITY_BINDING_TENANT:?wallet identity binding tenant is required}"
: "${WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE:?wallet identity binding key path is required}"
key=/run/secrets/wallet_identity_binding_key
if [ "$WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE" != "$key" ]; then
    printf '%s\n' 'wallet identity binding key path refused' >&2
    exit 1
fi
if [ "$(id -u)" -ne 0 ] || [ ! -d /run/secrets ] || [ -L /run/secrets ]; then
    printf '%s\n' 'wallet secret bootstrap unavailable' >&2
    exit 1
fi
chown root:node /run/secrets
chmod 0750 /run/secrets
if [ ! -f "$key" ] || [ -L "$key" ] || [ "$(stat -c %h "$key")" -ne 1 ]; then
    printf '%s\n' 'wallet identity binding key file refused' >&2
    exit 1
fi
key_size=$(stat -c %s "$key")
if [ "$key_size" -lt 1 ] || [ "$key_size" -gt 16384 ]; then
    printf '%s\n' 'wallet identity binding key size refused' >&2
    exit 1
fi
chown node:node "$key"
chmod 0600 "$key"
exec gosu node:node "$@"
