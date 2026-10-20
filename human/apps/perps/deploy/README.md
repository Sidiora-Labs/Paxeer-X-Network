# Hyperpax-UI — Production Deployment

Self-hosted Next.js origin, fronted by Cloudflare. This directory contains everything you need to bring a fresh Ubuntu box from "just SSH'd in" to "serving production traffic".

For the Cloudflare dashboard setup (DNS, SSL/TLS mode, Origin CA cert, WAF rules, rate limiting), see [CLOUDFLARE.md](./CLOUDFLARE.md).

---

## TL;DR (first-time deploy)

```bash
# 1. As root on the new box:
curl -fsSL https://raw.githubusercontent.com/Paxeer-Network/Hyperpax-UI/main/deploy/scripts/server-init.sh | sudo bash

# 2. Log out, log back in as the `deploy` user (created by step 1).
ssh deploy@<box-ip>

# 3. Clone the repo and configure env.
git clone https://github.com/Paxeer-Network/Hyperpax-UI.git ~/Hyperpax-UI
cd ~/Hyperpax-UI
cp deploy/.env.production.example .env.production
$EDITOR .env.production    # fill in the RPC URLs, indexer URLs, etc.

# 4. Drop the Cloudflare Origin CA cert at:
#    deploy/nginx/certs/origin.pem
#    deploy/nginx/certs/origin.key
#    (See CLOUDFLARE.md for how to mint these.)

# 5. First deploy.
./deploy/scripts/deploy.sh

# 6. Once the stack is healthy AND Cloudflare DNS is pointed at this box,
#    lock the firewall to Cloudflare-only:
sudo ./deploy/scripts/cloudflare-firewall.sh
```

---

## Architecture

```
                          ┌──────────────────────────────┐
   browser                │  Cloudflare edge             │
      │                   │  • WAF / DDoS / bot mgmt     │
      │  HTTPS            │  • Cache static assets       │
      ▼                   │  • Rate-limit per IP / route │
                          │  • TLS termination at edge   │
                          └──────────────┬───────────────┘
                                         │
                                         │ HTTPS (Full strict)
                                         ▼
                          ┌──────────────────────────────┐
   your box               │  nginx (port 443)            │
   (UFW: CF IPs only)     │  • Cloudflare Origin CA cert │
                          │  • set_real_ip_from CF       │
                          │  • Security headers          │
                          │  • Long-cache /_next/static  │
                          └──────────────┬───────────────┘
                                         │  proxy_pass app:3000
                                         ▼
                          ┌──────────────────────────────┐
                          │  Next.js standalone server   │
                          │  (Node 22, non-root)         │
                          └──────────────────────────────┘
```

Both containers run on a private Docker bridge network. The app container has no port published to the host — nginx is the only externally reachable surface.

---

## Files

```
deploy/
├── Dockerfile                # multi-stage build → ~300 MB image
├── docker-compose.yml        # app + nginx stack
├── .env.production.example   # template (copy to repo-root .env.production)
├── README.md                 # this file
├── CLOUDFLARE.md             # CF dashboard walkthrough
├── nginx/
│   ├── nginx.conf
│   ├── conf.d/
│   │   ├── 00-cloudflare-real-ip.conf   # CF IPs → real_ip_from
│   │   └── paxeer.conf                   # the actual server block
│   └── certs/                # CF Origin CA cert lives here (gitignored)
└── scripts/
    ├── server-init.sh                # one-time box provisioning
    ├── deploy.sh                     # idempotent build + recreate + health check
    └── cloudflare-firewall.sh        # narrow UFW to CF IPs only
```

---

## Step-by-step

### 1. Provision the box

`server-init.sh` is idempotent. Re-running is safe.

```bash
sudo bash deploy/scripts/server-init.sh
```

It installs Docker Engine + Compose v2, creates the `deploy` user, configures UFW (deny incoming, allow SSH + 80 + 443), enables fail2ban for SSH, sets up unattended security upgrades, configures Docker log rotation, and adds an 8 GB swapfile.

It does NOT change the SSH port or disable password auth. Those are operator decisions — recommend doing them once your key-based access is confirmed working:

```bash
# Optional hardening:
sudo sed -i 's/^#\?PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config
sudo systemctl restart ssh
```

### 2. Configure environment

Every variable lives in `.env.production` at the repo root (gitignored). The template (`deploy/.env.production.example`) lists them all with inline comments and **ships pre-filled with the public production defaults** (block-explorer URLs, Paxeer Wallet API, Supabase anon/publishable key). For most deploys, copying the template and only filling in your perps gateway + candle proxy is enough.

The non-negotiable ones for a working build:

- `NEXT_PUBLIC_PAXEER_RPC_URL` — primary RPC (template defaults to the public mainnet endpoint).
- `NEXT_PUBLIC_PERPS_GATEWAY_URL` — perps gateway origin (must set explicitly; the build fails at module load without it).
- `NEXT_PUBLIC_MARKET_PROXY_HOST` — Chainflow market feed host (optional, built-in default when blank).
- `NEXT_PUBLIC_PERPS_PRECOMPILE`, `NEXT_PUBLIC_LAUNCHPAD_PRECOMPILE`, `NEXT_PUBLIC_PECOR_MARKET_PRECOMPILE`, `NEXT_PUBLIC_PECOR_ORDERS_PRECOMPILE`, `NEXT_PUBLIC_PECOR_STOP_PRECOMPILE`, `NEXT_PUBLIC_PECOR_QUOTER_PRECOMPILE`, `NEXT_PUBLIC_PECOR_VAULT_PRECOMPILE` — optional precompile address overrides for a local node (release addresses when blank).
- `NEXT_PUBLIC_RELEASE_SPOT_OPEN` — spot release gate; `1` opens the spot route, its navigation entry and the markets spot mode (spot stays closed when blank).
- `PERPS_CANDLE_URL` + `MARKET_PROXY_HOST` — server-side candle proxy upstream (must set explicitly).

**Public vs secret keys.** A few `NEXT_PUBLIC_*` values look key-shaped but are not secrets — they're anon/publishable identifiers meant to be visible in the client bundle:

| Variable | Safe to expose? | Why |
|---|---|---|
| `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` | Yes | Anon key. Server-side wallet API validates the JWT separately. |
| `NEXT_PUBLIC_SUPABASE_URL` | Yes | Just a hostname. |
| `NEXT_PUBLIC_PAXEER_WALLET_API` | Yes | Public REST endpoint. |
| `NEXT_PUBLIC_PAXSCAN_*` | Yes | Public block explorer. |
| `MARKET_PROXY_HOST`, `PERPS_CANDLE_URL` | Yes (server-only) | Plain hostnames, but kept server-side to keep the proxy contract clean. |
| anything that looks like a service-role key, JWT signing secret, or wallet private key | **No** | Never put one of these in a `NEXT_PUBLIC_*` var — it gets baked into every client bundle. Use server-only env vars. |

**Critical:** if `NEXT_PUBLIC_SUPABASE_URL` or `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` is missing or blank at build time, the connect modal **silently hides the embedded-wallet sign-in section** and only injected wallets (MetaMask, Paxport extension, etc.) will be offered. The fix is to set both values and rebuild (`./deploy/scripts/deploy.sh`).

Anything starting with `NEXT_PUBLIC_*` is baked into the client bundle and requires a rebuild to change. The server-only vars (`PERPS_CANDLE_URL`, `MARKET_PROXY_HOST`) can change with a container restart.

### 3. Drop the Origin CA cert

See [CLOUDFLARE.md §2](./CLOUDFLARE.md#2-mint-an-origin-ca-certificate). You'll mint a cert from the Cloudflare dashboard and paste it into:

```
deploy/nginx/certs/origin.pem
deploy/nginx/certs/origin.key
```

The `certs/` directory is gitignored — don't commit them.

### 4. Deploy

```bash
./deploy/scripts/deploy.sh
```

This script:

1. `git pull --ff-only`.
2. Re-tags the current `hyperpax-ui:latest` image as `:previous` (rollback target).
3. Builds a new `:latest` image, baking the current commit SHA in.
4. Recreates the stack with `docker compose up -d`.
5. Waits up to 60 s for the app container's HEALTHCHECK to pass.
6. On health-check timeout, automatically rolls back to `:previous`.

Flags: `--no-pull`, `--no-build`, `--rollback`.

### 5. Lock the firewall to Cloudflare-only

Run **after** Cloudflare DNS is pointing at the box and you've verified the site loads through the orange cloud. If you run this before DNS is live, your origin becomes unreachable.

```bash
sudo ./deploy/scripts/cloudflare-firewall.sh
```

This fetches Cloudflare's published IP list, removes the existing wide-open allow rules, and adds per-CIDR allow rules for ports 80 and 443. Idempotent — safe to cron monthly so the rules stay current with any IP-list changes Cloudflare makes.

```cron
# /etc/cron.d/cloudflare-firewall
0 4 * * 1 root /home/deploy/Hyperpax-UI/deploy/scripts/cloudflare-firewall.sh > /var/log/cf-firewall.log 2>&1
```

### 6. Verify

```bash
# Container health
docker ps
docker logs hyperpax-app | tail -50
docker logs hyperpax-nginx | tail -50

# Local request through nginx
curl -sk -H 'Host: app.paxeer.app' https://127.0.0.1/api/build-id

# Through Cloudflare
curl -fsSL https://app.paxeer.app/api/build-id
```

`/api/build-id` returns the short commit SHA the image was built from. Useful for verifying which version is live.

---

## Routine operations

### Deploy a new version

From your laptop, push changes to `main`. Then on the box:

```bash
cd ~/Hyperpax-UI
./deploy/scripts/deploy.sh
```

Zero-downtime is approximate (nginx briefly drops new connections to the app during recreate). For true zero-downtime, run two app instances behind nginx and recreate them sequentially — out of scope for v1.

### Roll back

```bash
./deploy/scripts/deploy.sh --rollback
```

This restores the previous image (kept automatically by `deploy.sh`).

### Tail logs

```bash
docker compose -f deploy/docker-compose.yml logs -f
# Or just one service:
docker compose -f deploy/docker-compose.yml logs -f app
```

Logs are JSON-formatted, capped at 50 MB × 5 files per container.

### Restart without rebuilding

```bash
docker compose -f deploy/docker-compose.yml --env-file .env.production restart
```

Useful for picking up server-only env var changes without a fresh build.

---

## Hardening checklist

In rough priority order. Item 1 is non-negotiable; the rest are good-citizen.

1. **Firewall locked to Cloudflare IPs.** Run `cloudflare-firewall.sh` after DNS is live.
2. **SSH password auth disabled.** See §1 above.
3. **Cloudflare SSL/TLS mode set to "Full (strict)".** Free option, accepts the Origin CA cert.
4. **Cloudflare "Always Use HTTPS" and HSTS (Edge Certificates)** enabled.
5. **CF WAF: enable the Cloudflare Managed Ruleset.** Free tier.
6. **CF rate-limit rule for `/api/*`.** 60 req / 10 s per IP is a sensible starting point. The app-side proxy in `src/pages/api/candle/[...path].ts` also rate-limits, but CF's runs at the edge and survives an app restart.
7. **Unattended security upgrades enabled.** Already done by `server-init.sh`.
8. **Backup of `deploy/.env.production` + `deploy/nginx/certs/`** to a password manager / secret vault. These are not in git.
9. **Box monitoring.** At minimum, configure UptimeRobot or similar to hit `https://app.paxeer.app/api/build-id` every 60 s.

---

## Troubleshooting

### `deploy.sh` reports "healthcheck timed out"

The HEALTHCHECK hits `/api/build-id`. If that route fails, the container is up but the Next.js server is broken. Check logs:

```bash
docker logs hyperpax-app | tail -100
```

Most likely cause: a required `NEXT_PUBLIC_*` env var is missing and a top-level import threw. Verify `.env.production` against `.env.production.example`.

### nginx returns `502 Bad Gateway`

The app container isn't reachable. Check:

```bash
docker compose -f deploy/docker-compose.yml ps
docker logs hyperpax-app | tail -50
```

If the app is healthy, the issue is nginx config — likely you edited `conf.d/paxeer.conf` and `upstream` no longer resolves. Verify `proxy_pass http://app:3000;` is intact.

### Cloudflare returns `521 Web server is down`

Either your firewall is blocking CF (`cloudflare-firewall.sh` was run before DNS pointed here) or nginx isn't bound to 443 (cert files missing).

```bash
sudo ufw status | grep -E '(443|CLOUDFLARE)'
docker compose -f deploy/docker-compose.yml logs nginx | tail -30
```

To unblock quickly:

```bash
sudo ufw allow 443/tcp
sudo ufw allow 80/tcp
```

Then debug the cert / DNS issue at leisure.

### Connect-wallet modal shows injected wallets only — embedded sign-in section missing

Caused by `NEXT_PUBLIC_SUPABASE_URL` or `NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY` being empty *in the built bundle*. Two common reasons:

1. The values weren't in `.env.production` at the time of the original build.
2. The values are correct now, but a subsequent `deploy.sh` run hit the Docker layer cache and didn't actually re-bake the client bundle.

Diagnose:

```bash
# Returns chunk file paths if the key made it into the bundle.
docker exec hyperpax-app sh -c \
  'grep -rl "sb_publishable" /app/.next/static/chunks 2>/dev/null | head -3'
```

If that returns nothing, force a clean rebuild:

```bash
./deploy/scripts/deploy.sh --no-cache
```

Re-run the `grep` after the rebuild — it should now find at least one file. Then hard-reload the site in the browser (Cmd/Ctrl+Shift+R) to drop any stale service-worker cache.

The same `--no-cache` flag should be used any time you change `NEXT_PUBLIC_*` values in `.env.production` and want to be sure the new values actually land in the next build.

### Build is slow

The first build is slow (~3 min) because it has to pull pnpm + every dep. Subsequent builds use the Docker layer cache and finish in under 60 s for code-only changes. To force a clean build:

```bash
docker compose -f deploy/docker-compose.yml build --no-cache app
```

---

## What this setup deliberately doesn't include

- **No Let's Encrypt / certbot.** We use Cloudflare Origin CA (15-year cert, no renewal automation). If you ever want LE at the origin, drop certbot in and swap the cert paths in `conf.d/paxeer.conf`.
- **No Kubernetes / Nomad.** A single Docker Compose stack on one box is the right complexity level until you need multi-region active-active.
- **No CI-driven deploys yet.** `deploy.sh` is callable from a webhook or GitHub Action — but the trigger isn't wired up. The simplest next step is a GitHub Action that SSHes to the box and runs `deploy.sh`.
- **No automated backups.** This is a stateless frontend. There's nothing to back up except the env file and the CF Origin cert; both belong in a password manager, not in object storage.
