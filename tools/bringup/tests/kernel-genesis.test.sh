#!/bin/bash
# Renders a kernel genesis through tools/bringup/kernel-genesis.sh render from a
# fixture env file made of fresh keys, builds it with the repository's
# layerx-genesis-build, and checks the signed manifest enables the seven beta
# modules under the fixture sequencer and guarantor.
set -euo pipefail

root=$(cd "$(dirname "$0")/../../.." && pwd)
script=$root/tools/bringup/kernel-genesis.sh
export LAYERX_GENESIS_BUILD=${LAYERX_GENESIS_BUILD:-$root/build/bin/layerx-genesis-build}
work=$(mktemp -d /tmp/kernel-genesis-test.XXXXXX)
trap 'rm -rf "$work"' EXIT

fail() {
	printf 'kernel-genesis.test: %s\n' "$*" >&2
	exit 1
}
hex() { od -An -v -tx1 | tr -d ' \n'; }
ed25519_public() {
	python3 -c 'import sys; sys.stdout.buffer.write(bytes.fromhex("302e020100300506032b657004220420" + sys.argv[1]))' "$1" |
		openssl pkey -inform DER -pubout -outform DER | tail -c 32 | hex
}
sha() { printf '%s' "$1" | sha256sum | cut -d' ' -f1; }

[ -x "$LAYERX_GENESIS_BUILD" ] || fail "layerx-genesis-build is not built at $LAYERX_GENESIS_BUILD"

openssl rand -hex 32 | tr -d '\n' >"$work/sequencer.key"
sequencer_public=$(ed25519_public "$(cat "$work/sequencer.key")")
treasury_public=$(ed25519_public "$(openssl rand -hex 32)")
guarantor_public=$(openssl ecparam -name secp256k1 -genkey -noout |
	openssl ec -pubout -conv_form compressed -outform DER 2>/dev/null | tail -c 33 | hex)
guarantor_id=$(sha "layerx-beta-guarantor:$guarantor_public")
modules=(escrow budget stream service perps spot web)

fixture() {
	cat <<ENV
# kernel genesis fixture
LAYERX_GENESIS_NETWORK_ID=77
LAYERX_GENESIS_SEQUENCER_PUBLIC_KEY=$sequencer_public
LAYERX_GENESIS_SEQUENCER_KEY_FILE=$work/sequencer.key
LAYERX_GENESIS_TREASURY_PUBLIC_KEY=$treasury_public
LAYERX_GENESIS_GUARANTORS=$guarantor_id:$guarantor_public
LAYERX_GENESIS_ASSETS="PAX:$(sha layerx-asset:125:PAX):6 SID:$(sha layerx-asset:125:SID):18"
LAYERX_GENESIS_TIMESTAMP_MS=1790000000000
ENV
}
{ fixture; echo "LAYERX_GENESIS_MODULES=${modules[*]}"; } >"$work/genesis.env"

bash "$script" render "$work/genesis.env" "$work/out" >"$work/render.out" ||
	fail "render refused the fixture: $(cat "$work/render.out")"
manifest=$work/out/genesis/genesis.manifest
[ -s "$manifest" ] || fail "no genesis manifest"
grep -qx "sequencer_public_key=$sequencer_public" "$work/render.out" || fail "render did not report the fixture sequencer"
grep -qx "modules=budget escrow perps service spot stream web" "$work/render.out" || fail "render did not report the seven modules"

python3 - "$manifest" "$sequencer_public" "$guarantor_id" "$guarantor_public" "${modules[@]}" <<'PY'
import sys

manifest = open(sys.argv[1], 'rb').read()
sequencer, guarantor, guarantor_public = (bytes.fromhex(value) for value in sys.argv[2:5])
modules = sys.argv[5:]


def field(value):
    return len(value).to_bytes(4, 'big') + value


# The manifest ends with the signer public key and the signature, each a
# length-prefixed field (src/protocol/lxp_genesis.c lxp_genesis_encode).
if manifest[-104:-68] != field(sequencer) or manifest[-68:-64] != (64).to_bytes(4, 'big'):
    raise SystemExit('the manifest is not signed by the fixture sequencer')
for module in modules:
    row = field(('module-enable:' + module).encode().ljust(32, b'\0')) + field(bytes(31) + b'\x01')
    if manifest.count(row) != 1:
        raise SystemExit('the manifest does not enable ' + module)
if manifest.count(b'module-enable:') != len(modules):
    raise SystemExit('the manifest enables modules beyond the fixture list')
if manifest.count(field(guarantor) + field(guarantor_public)) != 1:
    raise SystemExit('the manifest does not carry the fixture guarantor')
PY

# Refusals: an unknown module, a sequencer key that is not the declared one,
# and bootstrap.sh's beta profile admits web but not beside explicit modules.
{ fixture; echo "LAYERX_GENESIS_MODULES=${modules[*]} asset"; } >"$work/unknown.env"
! bash "$script" render "$work/unknown.env" "$work/unknown" 2>"$work/unknown.err" || fail "render accepted an unknown module"
grep -q 'not asset' "$work/unknown.err" || fail "unknown module refusal: $(cat "$work/unknown.err")"
[ ! -e "$work/unknown" ] || fail "a refused render left output"
openssl rand -hex 32 | tr -d '\n' >"$work/sequencer.key"
! bash "$script" render "$work/genesis.env" "$work/mismatch" 2>"$work/mismatch.err" || fail "render accepted a foreign sequencer key"
grep -q 'does not hold the key' "$work/mismatch.err" || fail "sequencer refusal: $(cat "$work/mismatch.err")"
! bash "$root/platform/hosted/node/bootstrap.sh" --module-profile beta --enable-module web 2>"$work/profile.err" ||
	fail "bootstrap accepted --module-profile beside --enable-module"
grep -q -- '--module-profile excludes --enable-module' "$work/profile.err" || fail "bootstrap profile refusal: $(cat "$work/profile.err")"

echo "kernel-genesis.test: ok genesis_sha256=$(sha256sum "$manifest" | cut -d' ' -f1)"
