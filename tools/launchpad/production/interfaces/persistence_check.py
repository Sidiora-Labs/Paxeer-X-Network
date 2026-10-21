#!/usr/bin/env python3
"""Exercise the frozen persistence privilege contract on the local fixture only."""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import subprocess
import uuid


ROOT = Path(__file__).resolve().parents[4]
FROZEN_HISTORIES = {
    "core/indexer": ("indexer", "migration_control.indexer_versions"),
    "core/trading-charts": ("candles", "migration_control.trading_charts_versions"),
    "core/stats-workers": ("stats", "migration_control.stats_workers_versions"),
    "core/pnl-tracker": ("pnl", "migration_control.pnl_tracker_versions"),
    "media/user": ("users", "migration_control.user_versions"),
    "media/social": ("chat", "migration_control.social_versions"),
    "media/metadata": ("metadata", "migration_control.metadata_versions"),
    "media/livestream": ("livestream", "migration_control.livestream_versions"),
}


def run(args, sql=None):
    result = subprocess.run(args, input=sql, text=True, capture_output=True, timeout=90)
    if result.returncode:
        raise RuntimeError(f"local fixture command failed ({result.returncode}): {result.stderr.strip()}")
    return result.stdout


def validate_register(register):
    policy = register["runtime_role_policy"]
    if policy != {
        "owns_business_objects": False,
        "own_schema_grants": ["USAGE"],
        "own_table_grants": ["SELECT", "INSERT", "UPDATE", "DELETE"],
        "cross_schema_writes": False,
        "ddl": False,
        "migration_history_writes": False,
        "superuser": False,
        "createdb": False,
        "createrole": False,
        "bypassrls": False,
    }:
        raise ValueError("runtime role contract changed")
    ranges = register["ranges"]
    if len(ranges) != 50:
        raise ValueError("expected all 50 lane ranges")
    for n, entry in enumerate(sorted(ranges, key=lambda r: r["lane"]), 1):
        if (entry["lane"], entry["reserved_min"], entry["reserved_max"]) != (f"L{n:02d}", n * 10000, n * 10000 + 9999):
            raise ValueError("migration reservation mismatch")
    histories = register["histories"]
    if len(histories) != 8 or {h["service"] for h in histories} != set(FROZEN_HISTORIES):
        raise ValueError("expected eight current schema histories")
    for history in histories:
        if (history["schema"], history["proposed_history"]) != FROZEN_HISTORIES[history["service"]]:
            raise ValueError("history must match exact frozen schema and qualified identifier")
    tables = [h["proposed_history"] for h in histories]
    if len(set(tables)) != 8:
        raise ValueError("version tables are not isolated")
    if register["new_migration_entries"]:
        raise ValueError("interface freeze must not apply future service migrations")
    for history in histories:
        for migration in history["current_migrations"]:
            path = (ROOT / migration["path"]).resolve()
            if not path.is_relative_to(ROOT) or hashlib.sha256(path.read_bytes()).hexdigest() != migration["sha256"]:
                raise ValueError("legacy migration source checksum mismatch")
            version = migration["version"]
            if type(version) is not int or version < 1 or version > 2**63 - 1 or version != int(path.name.split("_")[0]):
                raise ValueError("legacy migration version must match source filename")
    return histories


def self_test(register):
    validate_register(register)
    invalid_names = [
        "migration_control.indexer_versions; DROP SCHEMA public CASCADE; --",
        "migration_control.indexer_versions --",
        "migration_control.indexer_versions.extra",
        'migration_control."indexer_versions"',
        "migration_control. indexer_versions",
        "migration_control.unregistered_versions",
        "public.indexer_versions",
        "migration_control.user_versions",
    ]
    for name in invalid_names:
        mutated = copy.deepcopy(register)
        mutated["histories"][0]["proposed_history"] = name
        try:
            validate_register(mutated)
        except ValueError:
            continue
        raise AssertionError("malformed or non-frozen SQL identifier accepted")
    print(json.dumps({"status": "pass", "identifier_rejections": len(invalid_names), "docker_access": False}))


def denied(statement):
    escaped = statement.replace("'", "''")
    return f"""DO $check$ BEGIN
      BEGIN EXECUTE '{escaped}';
        RAISE EXCEPTION 'privilege violation: unexpected success';
      EXCEPTION WHEN insufficient_privilege THEN NULL;
      END;
    END $check$;"""


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--compose", type=Path)
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--register", type=Path, default=ROOT / "launchpad/spec/production-system/integration/contracts/v1/migration-register.json")
    parser.add_argument("--runtime-source", type=Path)
    args = parser.parse_args()
    args.register = args.register.resolve()
    if not args.register.is_relative_to(ROOT):
        raise ValueError("migration register must belong to this checkout")
    runtime_source=(args.runtime_source or ROOT / "sdk/launchpad/shared/interfaces/v1/persistence.go").resolve()
    if not runtime_source.is_relative_to(ROOT) or not runtime_source.is_file():raise ValueError("runtime persistence source must belong to source checkout")
    if args.register != (ROOT / "launchpad/spec/production-system/integration/contracts/v1/migration-register.json").resolve() and args.runtime_source is None:raise ValueError("revised register requires explicit authoritative runtime source")
    register = json.loads(args.register.read_text())
    histories = validate_register(register)
    if args.self_test:
        self_test(register)
        return
    if args.compose is None:
        parser.error("--compose is required for the private PostgreSQL check")
    if os.environ.get("DOCKER_HOST") and not os.environ["DOCKER_HOST"].startswith("unix://"):
        raise ValueError("remote Docker is forbidden")
    context = json.loads(run(["docker", "context", "inspect"]))
    if not context[0]["Endpoints"]["docker"]["Host"].startswith("unix://"):
        raise ValueError("Docker context must use a local Unix socket")
    compose = args.compose.resolve()
    docker = ["docker", "compose", "--project-directory", str(compose.parent), "-f", str(compose)]
    config = json.loads(run(docker + ["config", "--format", "json"]))
    if config.get("name") != "kindle-bootstrap-local":
        raise ValueError("unexpected fixture project")
    ports = config["services"]["postgres"].get("ports", [])
    if len(ports) != 1 or ports[0].get("host_ip") != "127.0.0.1" or str(ports[0].get("published")) != "25432":
        raise ValueError("PostgreSQL fixture must be loopback25432")
    container = run(docker + ["ps", "-q", "postgres"]).strip()
    if not container or "\n" in container:
        raise ValueError("expected one running fixture PostgreSQL")
    inspection = json.loads(run(["docker", "inspect", container]))[0]
    if inspection["Config"]["Labels"].get("com.docker.compose.project") != "kindle-bootstrap-local":
        raise ValueError("container project differs")
    bindings = inspection["HostConfig"]["PortBindings"].get("5432/tcp", [])
    if bindings != [{"HostIp": "127.0.0.1", "HostPort": "25432"}]:
        raise ValueError("actual container port binding is not private fixture")
    if inspection["Config"]["Image"] != config["services"]["postgres"]["image"]:
        raise ValueError("fixture image differs from pinned configuration")
    prefix = "persistence_" + uuid.uuid4().hex[:16]
    database = prefix
    roles = [prefix + "_" + h["schema"] for h in histories]
    def psql(db, sql):
        return run(["docker", "exec", "-i", container, "psql", "-X", "-q", "-v", "ON_ERROR_STOP=1", "-U", "kindle_fixture", "-d", db], sql)
    created = False
    try:
        psql("kindle_fixture", f"CREATE DATABASE {database};")
        created = True
        setup = [f"REVOKE ALL ON DATABASE {database} FROM PUBLIC;", "REVOKE CREATE ON SCHEMA public FROM PUBLIC;", "CREATE SCHEMA migration_control;"]
        for history, role in zip(histories, roles):
            schema = history["schema"]
            setup += [f"CREATE ROLE {role} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS;", f"GRANT CONNECT ON DATABASE {database} TO {role};", f"CREATE SCHEMA {schema};", f"CREATE TABLE {schema}.privilege_probe(id integer PRIMARY KEY, value integer NOT NULL);", f"GRANT USAGE ON SCHEMA {schema} TO {role};", f"GRANT SELECT,INSERT,UPDATE,DELETE ON {schema}.privilege_probe TO {role};", f"CREATE TABLE {history['proposed_history']}(version_id bigint PRIMARY KEY, is_applied boolean NOT NULL);"]
            for migration in history["current_migrations"]:
                setup.append(f"INSERT INTO {history['proposed_history']} VALUES({migration['version']},true);")
        psql(database, "\n".join(setup))
        checks = 0
        for history, role in zip(histories, roles):
            schema = history["schema"]
            statements = [f"SET ROLE {role};", f"INSERT INTO {schema}.privilege_probe VALUES(1,1);", f"UPDATE {schema}.privilege_probe SET value=2 WHERE id=1;", f"DO $check$ BEGIN IF (SELECT value FROM {schema}.privilege_probe WHERE id=1) <> 2 THEN RAISE EXCEPTION 'own DML failed'; END IF; END $check$;", f"DELETE FROM {schema}.privilege_probe WHERE id=1;", f"DO $check$ BEGIN IF EXISTS (SELECT 1 FROM {schema}.privilege_probe WHERE id=1) THEN RAISE EXCEPTION 'own DELETE failed'; END IF; END $check$;"]
            negative = [f"CREATE TABLE {schema}.denied(id integer)", "CREATE TABLE public.denied(id integer)", "CREATE SCHEMA denied", f"ALTER TABLE {schema}.privilege_probe ADD COLUMN forbidden integer", f"DROP TABLE {schema}.privilege_probe", f"ALTER SCHEMA {schema} RENAME TO forbidden", f"DROP SCHEMA {schema} CASCADE", "CREATE ROLE forbidden_privilege_role", f"INSERT INTO {history['proposed_history']} VALUES(999999,true)"]
            for other in histories:
                if other["schema"] != schema:
                    target = other["schema"] + ".privilege_probe"
                    negative += [f"INSERT INTO {target} VALUES(1,1)", f"UPDATE {target} SET value=2", f"DELETE FROM {target}"]
            statements.extend(denied(sql) for sql in negative)
            statements.append("RESET ROLE;")
            psql(database, "\n".join(statements))
            checks += len(negative)
        for history in histories:
            count = len(history["current_migrations"])
            psql(database, f"DO $check$ BEGIN IF (SELECT count(*) FROM {history['proposed_history']}) <> {count} THEN RAISE EXCEPTION 'history isolation failed'; END IF; END $check$;")
        sources = [args.register, runtime_source, Path(__file__).resolve()]
        print(json.dumps({"status": "pass", "schema_count": 8, "migration_ranges": 50, "denied_sql_checks": checks, "own_dml_roles": 8, "history_tables": [h["proposed_history"] for h in histories], "register_sha256": hashlib.sha256(args.register.read_bytes()).hexdigest(), "source_sha256": {str(p.relative_to(ROOT)): hashlib.sha256(p.read_bytes()).hexdigest() for p in sources}, "scope": "isolated private fixture database; representative grant contract, not runtime migration qualification"}))
    finally:
        if created:
            psql("kindle_fixture", f"DROP DATABASE {database} WITH (FORCE);")
        for role in roles:
            psql("kindle_fixture", f"DROP ROLE IF EXISTS {role};")


if __name__ == "__main__":
    main()
