#!/usr/bin/env bash
# Renders the deploy-specific files (targets and alertmanager.yml) from a hosts env file.
# Usage: render.sh <hosts.env> <out-dir>
set -euo pipefail

env_file=${1:?usage: render.sh <hosts.env> <out-dir>}
out=${2:?usage: render.sh <hosts.env> <out-dir>}
here=$(cd "$(dirname "$0")" && pwd)

HOSTS="" COMET_METRICS_PORT=26660 NODE_EXPORTER_PORT=9100 EDGE_CERT_URLS=""
SEQUENCER_PRIMARY_READYZ_URLS="" SEQUENCER_STANDBY_READYZ_URLS="" AUTHORITY_METRICS_URLS=""
KEEPER_METRICS_URLS="" FEEDER_METRICS_URLS="" PAXAI_WORKER_METRICS_URLS="" AI_EVALUATOR_METRICS_URLS=""
X_WEBSEARCH_METRICS_URLS="" WALLET_ATTESTOR_METRICS_URLS="" EXPLORER_INDEX_HEAD_URLS="" STREAM_METER_PROVIDER_URLS=""
ALERT_WEBHOOK_URL="" ALERT_EMAIL_TO="" ALERT_EMAIL_FROM="" ALERT_SMTP_SMARTHOST="" ALERT_SMTP_USERNAME="" ALERT_SMTP_PASSWORD=""
# shellcheck disable=SC1090
. "$env_file"

[ -n "$HOSTS" ] || { echo "render.sh: HOSTS is empty in $env_file" >&2; exit 1; }
if [ -z "$ALERT_WEBHOOK_URL" ] && [ -z "$ALERT_EMAIL_TO" ]; then
	echo "render.sh: set ALERT_WEBHOOK_URL or ALERT_EMAIL_TO in $env_file" >&2
	exit 1
fi
if [ -n "$ALERT_EMAIL_TO" ] && { [ -z "$ALERT_EMAIL_FROM" ] || [ -z "$ALERT_SMTP_SMARTHOST" ]; }; then
	echo "render.sh: ALERT_EMAIL_TO needs ALERT_EMAIL_FROM and ALERT_SMTP_SMARTHOST" >&2
	exit 1
fi

mkdir -p "$out/targets" "$out/secrets"
cp "$here/prometheus.yml" "$out/prometheus.yml"

host_targets() { # port
	local sep="" pair name addr
	printf '['
	for pair in $HOSTS; do
		name=${pair%%=*} addr=${pair#*=}
		[ "$name" != "$pair" ] && [ -n "$name" ] && [ -n "$addr" ] || { echo "render.sh: bad HOSTS entry '$pair' (want name=address)" >&2; exit 1; }
		printf '%s{"targets":["%s:%s"],"labels":{"host":"%s"}}' "$sep" "$addr" "$1" "$name"
		sep=","
	done
	printf ']\n'
}

url_targets() { # list strip-scheme
	local sep="" url target
	printf '['
	for url in $1; do
		target=$url
		if [ "$2" = strip ]; then
			target=${url#https://}
			target=${target%%/*}
			case $target in *:*) ;; *) target=$target:443 ;; esac
		fi
		printf '%s{"targets":["%s"]' "$sep" "$target"
		if [ -n "${3:-}" ]; then
			printf ',"labels":{"role":"%s"}' "$3"
		fi
		printf '}'
		sep=","
	done
	printf ']\n'
}

host_targets "$COMET_METRICS_PORT" >"$out/targets/cometbft.json"
host_targets "$NODE_EXPORTER_PORT" >"$out/targets/node.json"
url_targets "$SEQUENCER_PRIMARY_READYZ_URLS" keep primary >"$out/targets/sequencer-primary-readyz.json"
url_targets "$SEQUENCER_STANDBY_READYZ_URLS" keep standby >"$out/targets/sequencer-standby-readyz.json"
url_targets "$AUTHORITY_METRICS_URLS" keep >"$out/targets/authority-metrics.json"
url_targets "$KEEPER_METRICS_URLS" keep >"$out/targets/keeper-metrics.json"
url_targets "$FEEDER_METRICS_URLS" keep >"$out/targets/feeder-metrics.json"
url_targets "$PAXAI_WORKER_METRICS_URLS" keep >"$out/targets/paxai-worker-metrics.json"
url_targets "$AI_EVALUATOR_METRICS_URLS" keep >"$out/targets/ai-evaluator-metrics.json"
url_targets "$X_WEBSEARCH_METRICS_URLS" keep >"$out/targets/x-websearch-metrics.json"
url_targets "$WALLET_ATTESTOR_METRICS_URLS" keep >"$out/targets/wallet-attestor-metrics.json"
url_targets "$EXPLORER_INDEX_HEAD_URLS" keep explorer-index-head >"$out/targets/explorer-index-head.json"
url_targets "$STREAM_METER_PROVIDER_URLS" keep stream-meter >"$out/targets/stream-meter-providers.json"
url_targets "$EDGE_CERT_URLS" strip >"$out/targets/certs.json"

receivers=""
if [ -n "$ALERT_WEBHOOK_URL" ]; then
	printf '%s' "$ALERT_WEBHOOK_URL" >"$out/secrets/webhook_url"
	receivers+="
    webhook_configs:
      - url_file: /etc/alertmanager/secrets/webhook_url
        send_resolved: true"
fi
if [ -n "$ALERT_EMAIL_TO" ]; then
	printf '%s' "$ALERT_SMTP_PASSWORD" >"$out/secrets/smtp_password"
	receivers+="
    email_configs:
      - to: '$ALERT_EMAIL_TO'
        from: '$ALERT_EMAIL_FROM'
        smarthost: '$ALERT_SMTP_SMARTHOST'
        auth_username: '$ALERT_SMTP_USERNAME'
        auth_password_file: /etc/alertmanager/secrets/smtp_password
        send_resolved: true"
fi
chmod 600 "$out"/secrets/* 2>/dev/null || true

cat >"$out/alertmanager.yml" <<YAML
route:
  receiver: fleet
  group_by: [alertname, host]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
receivers:
  - name: fleet$receivers
YAML
