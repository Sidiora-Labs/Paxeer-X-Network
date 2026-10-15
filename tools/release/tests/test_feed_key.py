#!/usr/bin/env python3
import os
import stat
import subprocess
import sys
import tempfile

RELEASE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, RELEASE)

import feed_key  # noqa: E402

TOOL = os.path.join(RELEASE, "feed_key.py")
GENERATOR = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"


def run(*args):
    return subprocess.run(
        [sys.executable, TOOL, *args], capture_output=True, text=True, check=False
    )


def decode_bytes_argument(calldata):
    assert int.from_bytes(calldata[4:36], "big") == 32
    length = int.from_bytes(calldata[36:68], "big")
    body = calldata[68:]
    assert len(body) % 32 == 0 and len(body) >= length
    assert body[length:] == b"\x00" * (len(body) - length)
    return body[:length]


def test_generator_vector_and_payload():
    assert (
        feed_key.keccak256(b"").hex()
        == "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"
    )
    assert feed_key.keccak256(b"transfer(address,uint256)")[:4].hex() == "a9059cbb"
    assert (
        feed_key.keccak256(b"a" * 200).hex()
        == "96ea54061def936c4be90b518992fdc6f12f535068a256229aca54267b4d084d"
    )
    assert feed_key.REGISTER_FEED_KEY_SELECTOR.hex() == "5d6617d9"

    assert feed_key.public_key(1).hex() == GENERATOR
    assert (
        feed_key.public_key(2).hex()
        == "02c6047f9441ed7d6d3045406e95c07cd85c778e4b8cef3ca7abac09b95c709ee5"
    )
    assert (
        feed_key.public_key(3).hex()
        == "02f9308a019258c31049344f85f89d5229b531c845836f99b08601f113bce036f9"
    )
    assert (
        feed_key.public_key(feed_key.N - 1).hex()
        == "0379be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
    )
    for bad in (0, feed_key.N):
        try:
            feed_key.public_key(bad)
        except ValueError:
            pass
        else:
            raise AssertionError(f"secret {bad} accepted")

    with tempfile.TemporaryDirectory() as scratch:
        key_path = os.path.join(scratch, "feed.key")
        result = run("new", "--out", key_path)
        assert result.returncode == 0, result.stderr
        assert stat.S_IMODE(os.stat(key_path).st_mode) == 0o600
        with open(key_path) as handle:
            text = handle.read().strip()
        assert len(text) == 64 and all(ch in "0123456789abcdef" for ch in text)
        printed = result.stdout.strip()
        assert len(printed) == 66 and printed[:2] in ("02", "03")
        assert printed == feed_key.public_key(int(text, 16)).hex()

        again = run("new", "--out", key_path)
        assert again.returncode == 1 and "File exists" in again.stderr
        with open(key_path) as handle:
            assert handle.read().strip() == text

        for pubkey in (GENERATOR, printed):
            result = run("payload", pubkey)
            assert result.returncode == 0, result.stderr
            out = result.stdout.strip()
            assert out.startswith("0x5d6617d9")
            calldata = bytes.fromhex(out[2:])
            assert len(calldata) == 4 + 32 + 32 + 64
            assert calldata[:4] == feed_key.REGISTER_FEED_KEY_SELECTOR
            assert decode_bytes_argument(calldata).hex() == pubkey

    for bad in (
        "zz" * 33,
        GENERATOR[:-2],
        "04" + GENERATOR[2:],
        "02" + "ff" * 32,
        "02" + "00" * 31 + "05",
    ):
        result = run("payload", bad)
        assert result.returncode == 1 and result.stdout == "", (bad, result.stdout)
        assert result.stderr.startswith("feed_key: public key"), (bad, result.stderr)


if __name__ == "__main__":
    test_generator_vector_and_payload()
    print("test_generator_vector_and_payload passed")
