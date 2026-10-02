#!/bin/sh
# Root init of the registry app (platform/hosted/registry/fly.toml). It does on
# the machine what platform/hosted/registry/node-provision-build-boundary.sh
# and the pod's init containers do on a Kubernetes node, without systemd:
# consumes retained request and publication tokens prepared on the volume and
# writes the supplied token secrets into /run/layerx, waits until deployment has put the
# TLS identities of tools/bringup/ca.sh issue registry and
# registry-event-client, the builder rootfs of
# builder-environment/build-env.sh, and the receipt authority replica id and
# sequencer trust history of the kernel app on the volume, loop-mounts one
# ext4 quota image per build slot, gives the registry a cgroup v2 subtree of
# its own with the host view of the hierarchy at
# LAYERX_REGISTRY_HOST_CGROUP_MOUNT, and starts the registry as root in a
# cgroup namespace rooted at that subtree; the registry delegates the subtree
# and drops to uid 4030 itself.
#
# Volume layout (/data):
#   /data/tls/<service>        identities of tools/bringup/ca.sh issue <service>
#   /data/builder              rootfs/ and environment-tree-digest of build-env.sh
#   /data/kernel               replica-id and trust-history read from the kernel app
#   /data/tokens               request and publication tokens of --prepare-material
#   /data/builds               slot-<n>.ext4 images mounted at slot-<n>
#   /data/state, /data/journal the registry's state and deployment journal
#
# Stage order of the deployment: material (ca.sh issue registry and
# registry-event-client from the one internal CA, the Fly secrets below, the
# builder environment and the kernel material) -> registry-bootstrap (this
# script) -> router-activation (human/wallet/deploy/endpoint.toml) ->
# routed-proof. This stage consumes only material: nothing here waits on,
# probes or names the router, so the registry comes up ready before the router
# is activated. Every consumed prerequisite is refused by name when absent,
# one line per item: "fail registry-bootstrap missing=<prerequisite>
# producer=<producer>", exit 1. A restart resumes with the volume as left;
# nothing here regenerates a CA, a client identity or a token another stage
# already holds.
set -eu
umask 077

log() { printf 'registry-init: %s\n' "$*" >&2; }
# missing <prerequisite> <producer>: the named refusal line of one item.
missing() { printf 'fail registry-bootstrap missing=%s producer=%s\n' "$1" "$2" >&2; }

mode=serve
case "${1:-}" in
--prepare-material) [ "$#" = 1 ] || exit 64; mode=material ;;
'') [ "$#" = 0 ] || exit 64 ;;
*) log "expected no arguments or --prepare-material"; exit 64 ;;
esac
for name in LAYERX_REGISTRY_REQUEST_TOKEN_FILE LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE LAYERX_REGISTRY_STATE LAYERX_REGISTRY_JOURNAL; do
	eval "path=\${$name:-}"
	case "$path" in /*) ;; *) missing "$name" "registry-deployment-material"; exit 1 ;; esac
done
tokens=$(dirname "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE")
[ "$(dirname "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE")" = "$tokens" ] &&
	[ "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE" != "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE" ] || {
	missing "distinct-token-pair-in-one-directory" "registry-deployment-material"; exit 1;
}
mkdir -p "$tokens" "$LAYERX_REGISTRY_STATE" "$LAYERX_REGISTRY_JOURNAL"

if [ -L "$tokens/.material.lock" ] || { [ -e "$tokens/.material.lock" ] && [ ! -f "$tokens/.material.lock" ]; }; then
	missing "material-lock" "the-retained-registry-volume"; exit 1
fi
exec 9>"$tokens/.material.lock"
flock -x -w 30 9 || { missing "material-lock" "registry-fly-init--prepare-material"; exit 1; }
if [ -L "$tokens/.initialized" ] || { [ -e "$tokens/.initialized" ] && [ ! -f "$tokens/.initialized" ]; }; then
	missing "token-material-marker" "the-retained-registry-volume"; exit 1
fi
retained_tokens=0
if [ -e "$tokens/.initialized" ] || [ -e "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE" ] ||
	[ -e "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE" ] ||
	[ -e "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE.new" ] ||
	[ -e "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE.new" ] ||
	[ -n "$(ls -A "$LAYERX_REGISTRY_STATE")" ] ||
	[ -n "$(ls -A "$LAYERX_REGISTRY_JOURNAL")" ]; then
	retained_tokens=1
fi

fresh() {
	if [ -L "$1" ] || [ -L "$1.new" ] || { [ -e "$1" ] && [ ! -f "$1" ]; }; then
		missing "$1" "the-retained-registry-volume"
		log "the retained token must be a regular file"
		exit 1
	fi
	if [ ! -s "$1" ] && [ "$retained_tokens" = 1 ]; then
		missing "$1" "the-retained-registry-volume"
		log "registry material already exists; restore the original token pair from the retained volume"
		exit 1
	fi
	[ -s "$1" ] || { openssl rand -hex 32 >"$1.new" && mv "$1.new" "$1"; }
}
if [ "$mode" = material ]; then
	fresh "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE"
	fresh "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE"
fi
for token in "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE" "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE"; do
	if [ -L "$token" ] || [ ! -s "$token" ] || [ ! -f "$token" ]; then
		missing "$token" "registry-fly-init--prepare-material"
		exit 1
	fi
	mode_bits=$(stat -c %a "$token")
	[ "$((0$mode_bits & 7))" = 0 ] || { missing "$token-mode" "registry-fly-init--prepare-material"; exit 1; }
	[ "$(stat -c %h "$token")" = 1 ] && [ "$(wc -c <"$token")" -le 4098 ] || {
		missing "$token-bounds" "registry-fly-init--prepare-material"; exit 1;
	}
done
if cmp -s "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE" "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE"; then
	log "the request and publication tokens are equal; restore the distinct original token pair"
	exit 1
fi
if [ "$mode" = material ]; then
	[ -e "$tokens/.initialized" ] || : >"$tokens/.initialized"
	log "material ready; retained request and publication tokens preserved"
	exit 0
fi
[ -f "$tokens/.initialized" ] && [ ! -L "$tokens/.initialized" ] || {
	missing "token-material-marker" "registry-fly-init--prepare-material"; exit 1;
}

flock -u 9
exec 9>&-

run=/run/layerx
builder=$(dirname "$LAYERX_REGISTRY_BUILDER_ENVIRONMENT_ROOT")
kernel=$(dirname "$LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY")
tokens=$(dirname "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE")
quota=$LAYERX_REGISTRY_BUILD_ROOT
slots=$LAYERX_REGISTRY_MAX_BUILDS
bytes=$LAYERX_REGISTRY_BUILD_QUOTA_BYTES
inodes=$LAYERX_REGISTRY_BUILD_QUOTA_INODES
case "$slots:$bytes:$inodes" in *[!0-9:]* | 0:* | *:0:* | *:0)
	log "the build slot count and quotas must be positive integers"
	exit 64
	;;
esac

mkdir -p "$run"
mountpoint -q "$run" || mount -t tmpfs -o nosuid,nodev,mode=0755 tmpfs "$run"
install -d -o 4030 -g 4030 -m 0700 "$run/secrets"
mkdir -p "$builder" "$kernel" "$quota"


# secret <variable> <file>: writes the Fly secret the variable carries to the
# file for uid 4030 and drops it from the environment the registry inherits.
secret() {
	eval "value=\${$1:-}"
	if [ -z "$value" ]; then
		missing "$1" "fly-secrets-import-of-the-registry-app"
		log "the Fly secret $1 is unset; import it as the deploy step says"
		exit 1
	fi
	printf '%s' "$value" >"$2"
	chown 4030:4030 "$2"
	chmod 0400 "$2"
	unset "$1" value
}
secret REGISTRY_IDENTITY_TOKEN "$LAYERX_REGISTRY_IDENTITY_TOKEN_FILE"
secret REGISTRY_PROGRAM_EVENTS_TOKEN "$LAYERX_EVENTS_PROGRAM_UPSTREAM_TOKEN_FILE"
secret REGISTRY_WEBHOOKS_EVENTS_TOKEN "$LAYERX_EVENTS_WEBHOOKS_UPSTREAM_TOKEN_FILE"
# bearer <variable>: the bearer the kernel app holds as well; refused by name
# when unset, never printed.
bearer() {
	eval "value=\${$1:-}"
	if [ -z "$value" ]; then
		missing "$1" "fly-secrets-import-of-the-registry-app-and-the-kernel-app"
		log "$2"
		exit 1
	fi
	unset value
}
bearer LAYERX_REGISTRY_NODE_AUTHORIZATION "the node bearer is a Fly secret of this app and of the kernel app"
bearer LAYERX_REGISTRY_RECEIPT_AUTHORITY_AUTHORIZATION "the receipt authority bearer is a Fly secret of this app and of the kernel app"

# wait_for <producer> <files...>: blocks until every file is non-empty or the
# material deadline passes; at the deadline every file still empty is refused
# by name with its producer and the script exits 1. A restart waits again with
# whatever the volume already holds.
material_deadline=$(($(date +%s) + 1800))
wait_for() {
	what=$1
	shift
	for file in "$@"; do
		if [ ! -s "$file" ]; then
			log "waiting for $file from $what"
			until [ -s "$file" ] || [ "$(date +%s)" -ge "$material_deadline" ]; do sleep 5; done
		fi
	done
	absent=0
	for file in "$@"; do
		[ -s "$file" ] || {
			missing "$file" "$(printf '%s' "$what" | tr ' ' '-')"
			absent=1
		}
	done
	[ "$absent" = 0 ] || exit 1
}
tls_dir=$(dirname "$LAYERX_REGISTRY_TLS_CERT_DER")
client_dir=$(dirname "$LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PKCS12")
wait_for "tools/bringup/ca.sh issue registry" "$LAYERX_REGISTRY_TLS_CERT_DER" "$LAYERX_REGISTRY_TLS_KEY_DER" "$LAYERX_REGISTRY_CLIENT_CA_DER"
wait_for "tools/bringup/ca.sh issue registry-event-client" "$LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PKCS12" "$LAYERX_REGISTRY_IDENTITY_CLIENT_IDENTITY_PASSWORD_FILE" "$client_dir/ca.der"
wait_for "the builder environment step of the deploy" "$builder/environment-tree-digest" "$LAYERX_REGISTRY_BUILDER_ENVIRONMENT_ROOT$LAYERX_REGISTRY_BUILDER_ENTRYPOINT"
wait_for "the kernel material step of the deploy" "$kernel/replica-id" "$LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY"

# One internal CA for every identity this app serves or presents: the CA every
# trust variable names and the root the event client identity was issued
# under are the CA of the registry identity. A different root means one of
# them came from another authority; it is refused, never replaced.
for trust in "$LAYERX_REGISTRY_OUTBOUND_CA_DER" "$LAYERX_REGISTRY_IDENTITY_CA_DER" \
	"$LAYERX_EVENTS_PROGRAM_UPSTREAM_CA_DER" "$LAYERX_EVENTS_WEBHOOKS_UPSTREAM_CA_DER" "$client_dir/ca.der"; do
	cmp -s "$trust" "$LAYERX_REGISTRY_CLIENT_CA_DER" || {
		missing "$trust=$LAYERX_REGISTRY_CLIENT_CA_DER" "tools/bringup/ca.sh-issue-from-the-one-internal-CA"
		log "$trust is not the internal CA of $LAYERX_REGISTRY_CLIENT_CA_DER"
		exit 1
	}
done

replica=$(tr -d ' \r\n' <"$kernel/replica-id")
case "$replica" in
*[!0-9a-f]* | '')
	log "$kernel/replica-id is not lowercase hex"
	exit 1
	;;
esac
[ "${#replica}" -eq 64 ] || {
	log "$kernel/replica-id is not 32 bytes"
	exit 1
}
export LAYERX_REGISTRY_RECEIPT_AUTHORITY_REPLICA_ID="$replica"
LAYERX_REGISTRY_BUILDER_IMAGE_DIGEST=$(tr -d ' \r\n' <"$builder/environment-tree-digest")
LAYERX_REGISTRY_BUILDER_ISOLATION_RUNTIME_DIGEST=$(sha256sum "$LAYERX_REGISTRY_BUILDER_ISOLATION_RUNTIME" | cut -d' ' -f1)
LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR_DIGEST=$(sha256sum "$LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR" | cut -d' ' -f1)
export LAYERX_REGISTRY_BUILDER_IMAGE_DIGEST LAYERX_REGISTRY_BUILDER_ISOLATION_RUNTIME_DIGEST LAYERX_REGISTRY_BUILDER_JOB_SUPERVISOR_DIGEST

# The build boundary of node-provision-build-boundary.sh: one ext4 image per
# slot with the byte and inode quota, loop-mounted nosuid,nodev,noatime with
# autoclear at <quota>/slot-<n>, owned 4030 with mode 0700.
groups=$(((bytes + 32768 * 4096 - 1) / (32768 * 4096)))
inodes_per_group=$((inodes / groups / 16 * 16))
[ "$inodes_per_group" -ge 16 ] || {
	log "the inode quota is too small for the byte quota"
	exit 64
}
slot=0
while [ "$slot" -lt "$slots" ]; do
	image="$quota/slot-$slot.ext4"
	target="$quota/slot-$slot"
	mkdir -p "$target"
	if ! mountpoint -q "$target"; then
		if [ -e "$image" ]; then
			[ "$(stat -c %s "$image")" = "$bytes" ] || {
				log "$image does not hold $bytes bytes"
				exit 67
			}
			e2fsck -p "$image" || [ "$?" = 1 ]
		else
			truncate -s "$bytes" "$image.new"
			mkfs.ext4 -q -F -b 4096 -g 32768 -I 256 -N "$((inodes_per_group * groups))" "$image.new"
			mv "$image.new" "$image"
		fi
		mount -t ext4 -o loop,nosuid,nodev,noatime "$image" "$target"
	fi
	loop=$(findmnt -n --first-only -o SOURCE --target "$target")
	case "$loop" in /dev/loop[0-9]*) ;; *)
		log "$target is not a loop mount"
		exit 67
		;;
	esac
	[ "$(losetup -n -O AUTOCLEAR "$loop" | tr -d '[:space:]')" = 1 ]
	[ "$(findmnt -n --first-only -o FSTYPE --target "$target")" = ext4 ]
	options=$(findmnt -n --first-only -o OPTIONS --target "$target")
	for option in rw nosuid nodev noatime; do
		case ",$options," in *",$option,"*) ;; *)
			log "$target lacks mount option $option"
			exit 67
			;;
		esac
	done
	[ "$(stat -c %d "$target")" != "$(stat -c %d "$quota")" ]
	chmod 0700 "$target"
	chown 4030:4030 "$target"
	slot=$((slot + 1))
done

chown -R 4030:4030 "$tls_dir" "$client_dir" "$tokens" "$LAYERX_REGISTRY_STATE" "$LAYERX_REGISTRY_JOURNAL" "$kernel"
chmod 0400 "$LAYERX_REGISTRY_REQUEST_TOKEN_FILE" "$LAYERX_REGISTRY_PUBLICATION_TOKEN_FILE" "$LAYERX_REGISTRY_SEQUENCER_TRUST_HISTORY"
# ca.sh makes the certificate root under umask 077; uid 4030 only traverses it.
chmod 0711 "$(dirname "$tls_dir")"

# The cgroup v2 subtree the Kubernetes node gives the pod: controllers enabled
# at the root, a registry cgroup this process moves into, and the host view of
# the hierarchy bound where the registry finds its own cgroup by inode.
cgroup=/sys/fs/cgroup
mountpoint -q "$cgroup" || mount -t cgroup2 -o nosuid,nodev,noexec cgroup2 "$cgroup"
for controller in cpu memory pids io; do
	grep -qw "$controller" "$cgroup/cgroup.controllers" || {
		log "the machine's cgroup v2 hierarchy lacks the $controller controller"
		exit 1
	}
done
echo "+cpu +memory +pids +io" >"$cgroup/cgroup.subtree_control"
mkdir -p "$cgroup/registry" "$run/host-cgroup"
mountpoint -q "$run/host-cgroup" || mount --bind "$cgroup" "$run/host-cgroup"
echo "$$" >"$cgroup/registry/cgroup.procs"
exec unshare --cgroup --mount --propagation private -- /bin/sh -c \
	"mount -t cgroup2 -o nosuid,nodev,noexec cgroup2 $cgroup && exec /usr/local/bin/layerx-program-registry"
