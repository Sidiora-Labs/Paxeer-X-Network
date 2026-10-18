# Isolated bootstrap fixtures

This stack is local integration infrastructure, not a production deployment or
Paxeer consensus benchmark. Its Ganache EVM uses chain ID 125 and instant local
mining; it does not reproduce Paxeer's validator, finality, transaction extension,
or performance implementation. The publicly known accounts and fixed credentials
are test data. Never fund these accounts outside this fixture.

All host ports bind to `127.0.0.1`. The private Compose bridge disables outbound
IP masquerading. This is not an absolute egress firewall: access to reachable host
or private-network routes may still be possible. Docker internal networks suppress
published ports on Docker 29, so they are unsuitable for host-run integration tests.
A local privileged user can still access Docker and these test services.
No existing service, production volume, `.env`, or chain data is mounted.

The coordinator starts this stack once, after reviewing this configuration:

```sh
docker compose -f deploy/production/bootstrap/compose.yml config --quiet
python3 deploy/production/bootstrap/validate.py
python3 deploy/production/bootstrap/start.py
python3 deploy/production/bootstrap/smoke.py
```

`start.py` starts and waits for the five persistent services, then runs the profiled
`minio-init` one-shot bucket initializer and requires its exit code to be zero.
This avoids incorrectly treating successful initializer termination as a failed
long-running service health check. An initialization failure fails startup. Other services
have application-level Docker health checks. NATS uses persistent JetStream R1
for this small bootstrap fixture. Production R3, multi-zone failure, capacity and
recovery qualification remain later requirements and are not demonstrated here.
Redis is deliberately ephemeral. PostgreSQL, broker, object and EVM state use
project-scoped named volumes. Restart preserves fixture data.

Host connection strings are in `fixture.env.example`. This file contains no
production secrets and should not be copied over any application environment.
Containers on this fixture network use service DNS and container ports instead.
The chain starts without Sidiora contracts: contract deployment and application
address configuration remain explicit coordinator prerequisites. Existing storage
integration tests may launch their own Testcontainers rather than consume this
stack; the fixture alone does not change those test configurations.

Every image is digest-pinned. PostgreSQL and Redis pins were verified against the
local official-image cache. NATS 2.11.9-alpine and Ganache 7.9.2 manifests were
verified from their upstream Docker registry. MinIO server digest resolves to the
cached official RELEASE.2025-09-07T16-13-09Z image; its historical registry tag is
not available from this environment. The server and `mc` pins are usable from the
existing local cache, but availability on a fresh machine must be verified before
calling cold-host reproduction qualified. Foundry registry access was denied, so
this fixture uses the available upstream Ganache image instead of an unverified
Anvil reference. Image provenance details are in `images.json`.

Stop and preserve state:

```sh
docker compose -f deploy/production/bootstrap/compose.yml down
```

Deleting fixture volumes is a separate destructive local cleanup decision; the
provided scripts never remove volumes. Concurrent independent fixture stacks
require explicitly reviewed project names and host-port overrides; the default
stack is intentionally coordinator-owned to prevent 50 agents starting copies.
