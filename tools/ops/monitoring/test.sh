#!/usr/bin/env bash
# Renders the stack from hosts.env.example and checks it with promtool and amtool.
# Uses local promtool/amtool when present, otherwise the pinned images through Docker.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

"$here/render.sh" "$here/hosts.env.example" "$work/rendered"
cp "$here/rules.yml" "$here/rules_test.yml" "$here/blackbox.yml" "$work/"
# Exercise the email route too.
sed -e 's/^ALERT_EMAIL_TO=.*/ALERT_EMAIL_TO="ops@example.com"/' \
	-e 's/^ALERT_EMAIL_FROM=.*/ALERT_EMAIL_FROM="alerts@example.com"/' \
	-e 's/^ALERT_SMTP_SMARTHOST=.*/ALERT_SMTP_SMARTHOST="smtp.example.com:587"/' \
	"$here/hosts.env.example" >"$work/email.env"
"$here/render.sh" "$work/email.env" "$work/rendered-email"
chmod -R a+rX "$work"

prom() {
	if command -v promtool >/dev/null 2>&1; then
		(cd "$work" && promtool "$@")
	else
		docker run --rm --entrypoint promtool -w /work \
			-v "$work:/work:ro" \
			-v "$work/rendered/targets:/etc/prometheus/targets:ro" \
			-v "$work/rules.yml:/etc/prometheus/rules.yml:ro" \
			prom/prometheus:v2.55.1 "$@"
	fi
}
am() {
	if command -v amtool >/dev/null 2>&1; then
		(cd "$work" && amtool "$@")
	else
		docker run --rm --entrypoint amtool -w /work -v "$work:/work:ro" prom/alertmanager:v0.27.0 "$@"
	fi
}

prom check rules rules.yml
prom test rules rules_test.yml
if command -v promtool >/dev/null 2>&1; then
	echo "local promtool: checking config syntax only (file_sd paths are container paths)"
	prom check config --syntax-only rendered/prometheus.yml
else
	prom check config rendered/prometheus.yml
fi
am check-config rendered/alertmanager.yml
am check-config rendered-email/alertmanager.yml
docker run --rm --entrypoint /bin/blackbox_exporter -v "$work/blackbox.yml:/b.yml:ro" \
	prom/blackbox-exporter:v0.25.0 --config.file=/b.yml --config.check
echo "monitoring: all checks passed"
