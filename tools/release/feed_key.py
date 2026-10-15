#!/usr/bin/env python3
"""Generates price-feed keys and the registerFeedKey(bytes) calldata of the 0x1020 precompile."""

import argparse
import os
import sys

P = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEFFFFFC2F
N = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFEBAAEDCE6AF48A03BBFD25E8CD0364141
G = (
    0x79BE667EF9DCBBAC55A06295CE870B07029BFCDB2DCE28D959F2815B16F81798,
    0x483ADA7726A3C4655DA4FBFC0E1108A8FD17B448A68554199C47D08FFB10D4B8,
)

KECCAK_ROUND_CONSTANTS = (
    0x0000000000000001,
    0x0000000000008082,
    0x800000000000808A,
    0x8000000080008000,
    0x000000000000808B,
    0x0000000080000001,
    0x8000000080008081,
    0x8000000000008009,
    0x000000000000008A,
    0x0000000000000088,
    0x0000000080008009,
    0x000000008000000A,
    0x000000008000808B,
    0x800000000000008B,
    0x8000000000008089,
    0x8000000000008003,
    0x8000000000008002,
    0x8000000000000080,
    0x000000000000800A,
    0x800000008000000A,
    0x8000000080008081,
    0x8000000000008080,
    0x0000000080000001,
    0x8000000080008008,
)
KECCAK_ROTATIONS = (
    (0, 36, 3, 41, 18),
    (1, 44, 10, 45, 2),
    (62, 6, 43, 15, 61),
    (28, 55, 25, 21, 56),
    (27, 20, 39, 8, 14),
)
MASK64 = (1 << 64) - 1
KECCAK_RATE = 136


def _rotl(value, shift):
    return ((value << shift) | (value >> (64 - shift))) & MASK64 if shift else value


def _keccak_f(state):
    for rc in KECCAK_ROUND_CONSTANTS:
        c = [
            state[x][0] ^ state[x][1] ^ state[x][2] ^ state[x][3] ^ state[x][4]
            for x in range(5)
        ]
        d = [c[(x - 1) % 5] ^ _rotl(c[(x + 1) % 5], 1) for x in range(5)]
        state = [[state[x][y] ^ d[x] for y in range(5)] for x in range(5)]
        b = [[0] * 5 for _ in range(5)]
        for x in range(5):
            for y in range(5):
                b[y][(2 * x + 3 * y) % 5] = _rotl(state[x][y], KECCAK_ROTATIONS[x][y])
        state = [
            [b[x][y] ^ ((~b[(x + 1) % 5][y]) & b[(x + 2) % 5][y]) for y in range(5)]
            for x in range(5)
        ]
        state[0][0] ^= rc
    return state


def keccak256(data):
    """Keccak-256 with the original 0x01 padding used by the EVM, not SHA3-256."""
    padded = bytearray(data)
    padded.append(0x01)
    padded.extend(b"\x00" * (-len(padded) % KECCAK_RATE))
    padded[-1] |= 0x80
    state = [[0] * 5 for _ in range(5)]
    for offset in range(0, len(padded), KECCAK_RATE):
        block = padded[offset : offset + KECCAK_RATE]
        for i in range(KECCAK_RATE // 8):
            state[i % 5][i // 5] ^= int.from_bytes(block[8 * i : 8 * i + 8], "little")
        state = _keccak_f(state)
    return b"".join(state[i % 5][i // 5].to_bytes(8, "little") for i in range(4))


def _point_add(a, b):
    if a is None:
        return b
    if b is None:
        return a
    if a[0] == b[0]:
        if (a[1] + b[1]) % P == 0:
            return None
        slope = 3 * a[0] * a[0] * pow(2 * a[1], -1, P) % P
    else:
        slope = (b[1] - a[1]) * pow(b[0] - a[0], -1, P) % P
    x = (slope * slope - a[0] - b[0]) % P
    return x, (slope * (a[0] - x) - a[1]) % P


def _scalar_mult(k, point):
    result = None
    while k:
        if k & 1:
            result = _point_add(result, point)
        point = _point_add(point, point)
        k >>= 1
    return result


def public_key(secret):
    """The 33-byte compressed secp256k1 public key of a secret in [1, N-1]."""
    if not 1 <= secret < N:
        raise ValueError("secret must be in [1, n-1] of secp256k1")
    x, y = _scalar_mult(secret, G)
    return bytes([2 + (y & 1)]) + x.to_bytes(32, "big")


def parse_public_key(text):
    """Decodes 66 hex digits of a compressed secp256k1 point and checks it lies on the curve."""
    try:
        raw = bytes.fromhex(text.strip().removeprefix("0x"))
    except ValueError:
        raise ValueError("public key is not hex") from None
    if len(raw) != 33 or raw[0] not in (2, 3):
        raise ValueError("public key must be 33 bytes starting 02 or 03")
    x = int.from_bytes(raw[1:], "big")
    if x >= P:
        raise ValueError("public key x is not below the field prime")
    rhs = (pow(x, 3, P) + 7) % P
    if pow(pow(rhs, (P + 1) // 4, P), 2, P) != rhs:
        raise ValueError("public key is not a secp256k1 point")
    return raw


def random_secret():
    """A uniform secret in [1, N-1] by rejection sampling of os.urandom."""
    while True:
        secret = int.from_bytes(os.urandom(32), "big")
        if 1 <= secret < N:
            return secret


def write_key(path, secret):
    """Writes the secret as 64 lowercase hex digits, the load_key file format, with mode 0600; never overwrites."""
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        os.fchmod(fd, 0o600)
        os.write(fd, f"{secret:064x}\n".encode())
    finally:
        os.close(fd)


REGISTER_FEED_KEY_SELECTOR = keccak256(b"registerFeedKey(bytes)")[:4]


def register_payload(pubkey):
    """The ABI calldata of registerFeedKey(bytes pubKey): selector, offset, length, padded bytes."""
    padded = pubkey + b"\x00" * (-len(pubkey) % 32)
    return (
        REGISTER_FEED_KEY_SELECTOR
        + (32).to_bytes(32, "big")
        + len(pubkey).to_bytes(32, "big")
        + padded
    )


def new(out):
    secret = random_secret()
    write_key(out, secret)
    return public_key(secret)


def payload(pubkey_hex):
    return register_payload(parse_public_key(pubkey_hex))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    new_cmd = commands.add_parser(
        "new", help="write a fresh feed key and print its compressed public key"
    )
    new_cmd.add_argument(
        "--out", required=True, help="key file to create (refused if it exists)"
    )
    payload_cmd = commands.add_parser(
        "payload", help="print the registerFeedKey(bytes) calldata for a public key"
    )
    payload_cmd.add_argument(
        "pubkey", help="33-byte compressed public key as 66 hex digits"
    )
    args = parser.parse_args(argv)
    try:
        if args.command == "new":
            print(new(args.out).hex())
        else:
            print("0x" + payload(args.pubkey).hex())
    except (OSError, ValueError) as err:
        print(f"feed_key: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
