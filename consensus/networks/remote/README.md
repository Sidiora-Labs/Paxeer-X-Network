# Remote Cluster with Terraform and Ansible

Upstream Tendermint tooling. [`terraform/`](./terraform/) provisions DigitalOcean droplets and [`ansible/`](./ansible/) installs, configures, starts, stops and resets a standalone `tendermint` service on them; [`integration.sh`](./integration.sh) drives both from a fresh droplet.

This repository does not build a standalone `tendermint` binary: the engine is embedded in `paxd`. The tooling is therefore not runnable as-is. See [`../../README.md`](../../README.md) for how the engine is built here.
