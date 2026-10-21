"""Offline recovery contracts. No credentials, network clients or command execution.

Manifests are content-bound, not independently authenticated: the coordinator must
pin their digest in its trusted inventory before permitting any restore.
"""
from __future__ import annotations
import hashlib
import json
import os
import re
from pathlib import Path

MAX_JSON = 2 * 1024 * 1024
HEX = re.compile(r"[0-9a-f]{64}\Z")
NAME = re.compile(r"[a-z][a-z0-9-]{1,62}\Z")
EVENT = re.compile(r"125:([0-9a-f]{64}):([0-9]+):([0-9]+)\Z")
REQUIRED_TABLES = frozenset({"business", "outbox", "inbox", "watermarks", "recovery_control"})
METRICS = {
 "D14_031": "backup_wal_archive_age_seconds",
 "D14_032": "dr_recovery_elapsed_seconds",
 "D14_033": "restore_epoch_rejections_total",
 "D14_034": "recovery_outbox_pending_rows",
 "D14_035": "recovery_replayed_events_total",
 "D14_036": "archive_checksum_failures_total",
 "D14_037": "restore_drill_last_success_timestamp",
 "D14_038": "backup_access_denied_total",
 "D14_039": "recovery_missing_objects_total",
 "D14_040": "recovery_active_region_epoch",
}

class Invalid(ValueError):
    pass

def require(ok, message):
    if not ok:
        raise Invalid(message)

def exact(obj, keys):
    require(isinstance(obj, dict) and set(obj) == set(keys), "unknown or missing fields")

def integer(value, low=0, high=2**63-1):
    require(type(value) is int and low <= value <= high, "integer out of bounds")
    return value

def digest(value):
    require(isinstance(value, str) and HEX.fullmatch(value), "invalid SHA256")
    return value

def name(value):
    require(isinstance(value, str) and NAME.fullmatch(value), "invalid resource name")
    return value

def canonical(obj):
    return json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()

def sha(obj):
    return hashlib.sha256(canonical(obj)).hexdigest()

def load(raw):
    require(isinstance(raw, bytes) and len(raw) <= MAX_JSON, "JSON size exceeded")
    def pairs(items):
        obj = {}
        for key, value in items:
            require(key not in obj, "duplicate JSON field")
            obj[key] = value
        return obj
    try:
        return json.loads(raw, object_pairs_hook=pairs,
                          parse_constant=lambda _: (_ for _ in ()).throw(Invalid("nonfinite JSON")))
    except (UnicodeDecodeError, json.JSONDecodeError, RecursionError) as exc:
        raise Invalid("invalid JSON") from exc

def configuration(c):
    exact(c, {"version", "environment", "primary_region", "recovery_region", "archive_bucket",
              "replica_bucket", "backup_principal", "restore_principal", "retention_principal",
              "application_principal", "kms_key", "replica_kms_key", "postgres_major",
              "wal_age_seconds", "replica_lag_seconds", "rpo_seconds", "rto_seconds",
              "base_backup_seconds", "daily_points", "monthly_points", "archive_days", "hot_hours"})
    require(c["version"] == 1 and type(c["version"]) is int, "unsupported config version")
    for key in ("environment", "primary_region", "recovery_region", "archive_bucket", "replica_bucket",
                "backup_principal", "restore_principal", "retention_principal", "application_principal"):
        name(c[key])
    require(c["primary_region"] != c["recovery_region"], "independent region required")
    require(c["archive_bucket"] != c["replica_bucket"], "independent buckets required")
    principals = [c[x] for x in ("backup_principal", "restore_principal", "retention_principal", "application_principal")]
    require(len(set(principals)) == 4, "separate principals required")
    for key in ("kms_key", "replica_kms_key"):
        require(isinstance(c[key], str) and re.fullmatch(r"kms://[a-z0-9-]+/[a-z0-9-]+", c[key]), "KMS reference required")
    require(c["kms_key"] != c["replica_kms_key"], "independent environment keys required")
    integer(c["postgres_major"], 16, 18)
    for key, high in (("wal_age_seconds", 60), ("replica_lag_seconds", 300), ("rpo_seconds", 300),
                      ("rto_seconds", 1800), ("base_backup_seconds", 86400)):
        integer(c[key], 1, high)
    require(c["daily_points"] == 35 and c["monthly_points"] == 12 and c["archive_days"] == 365
            and c["hot_hours"] == 72 and all(type(c[k]) is int for k in
            ("daily_points", "monthly_points", "archive_days", "hot_hours")), "retention policy review required")
    return c

def checkpoint(c):
    exact(c, {"chain_id", "height", "block_hash", "epoch", "source_sha256", "deployment_sha256", "interface_sha256"})
    require(type(c["chain_id"]) is int and c["chain_id"] == 125, "wrong chain")
    integer(c["height"]); integer(c["epoch"], 1)
    for key in ("block_hash", "source_sha256", "deployment_sha256", "interface_sha256"):
        digest(c[key]); require(c[key] != "0"*64, "zero provenance")
    return c

def manifest(m):
    exact(m, {"version", "environment", "region", "backup_id", "postgres_system_id", "postgres_major",
              "timeline", "start_lsn", "end_lsn", "created_unix", "snapshot_id", "checkpoint",
              "tables", "objects", "inventory_sha256", "encryption_key", "wal_complete_through_unix"})
    require(type(m["version"]) is int and m["version"] == 1, "unsupported manifest version")
    for k in ("environment", "region", "backup_id", "snapshot_id"): name(m[k])
    require(isinstance(m["postgres_system_id"], str) and re.fullmatch(r"[1-9][0-9]{0,19}", m["postgres_system_id"]), "system identity required")
    integer(m["postgres_major"], 16, 18); integer(m["timeline"], 1, 2**32-1)
    def lsn(v):
        require(isinstance(v, str) and re.fullmatch(r"[0-9A-F]{1,8}/[0-9A-F]{1,8}", v), "invalid LSN")
        a,b=v.split("/"); return (int(a,16)<<32) + int(b,16)
    require(lsn(m["start_lsn"]) <= lsn(m["end_lsn"]), "backwards WAL")
    integer(m["created_unix"], 1); integer(m["wal_complete_through_unix"], m["created_unix"])
    checkpoint(m["checkpoint"])
    require(isinstance(m["tables"], dict) and set(m["tables"]) == REQUIRED_TABLES, "inconsistent snapshot inventory")
    for value in m["tables"].values():
        exact(value, {"count", "sha256", "snapshot_id"}); integer(value["count"])
        digest(value["sha256"]); require(value["snapshot_id"] == m["snapshot_id"], "mixed database snapshots")
    require(isinstance(m["objects"], list) and 1 <= len(m["objects"]) <= 10000, "object inventory bound")
    seen = set()
    for obj in m["objects"]:
        exact(obj, {"key", "version_id", "sha256", "size", "kind"})
        object_key(obj["key"]); name(obj["version_id"]); digest(obj["sha256"]); integer(obj["size"],1, 2**40)
        require(obj["kind"] in {"base", "wal", "raw-chain", "media", "metadata"}, "unknown object kind")
        require(obj["key"] not in seen, "duplicate object"); seen.add(obj["key"])
    require({"base", "wal"} <= {o["kind"] for o in m["objects"]}, "physical base and WAL inventory required")
    digest(m["inventory_sha256"])
    require(m["inventory_sha256"] == sha({"tables":m["tables"],"objects":m["objects"]}), "inventory digest mismatch")
    require(isinstance(m["encryption_key"],str) and re.fullmatch(r"kms://[a-z0-9-]+/[a-z0-9-]+",m["encryption_key"]),"encrypted backup required")
    return m

def object_key(key):
    require(isinstance(key,str) and 1 <= len(key) <= 512 and re.fullmatch(r"[a-zA-Z0-9._/-]+",key), "invalid object key")
    require(not key.startswith("/") and all(x not in {"", ".", ".."} for x in key.split("/")), "unsafe object key")
    return key

def restore_plan(c, m, trusted_manifest_sha256, target_unix, expected_system_id):
    configuration(c); manifest(m); digest(trusted_manifest_sha256)
    require(sha(m) == trusted_manifest_sha256, "untrusted manifest")
    require(m["environment"] == c["environment"] and m["region"] == c["primary_region"], "wrong source environment")
    require(m["postgres_major"] == c["postgres_major"] and m["postgres_system_id"] == expected_system_id, "wrong PostgreSQL identity")
    require(m["encryption_key"] == c["kms_key"], "wrong backup key")
    integer(target_unix,m["created_unix"],m["wal_complete_through_unix"])
    return {"version":1,"mode":"review-required-offline-plan", "manifest_sha256":sha(m),
            "target_unix":target_unix,"target_region":c["recovery_region"],
            "new_epoch":integer(m["checkpoint"]["epoch"]+1,1),
            "postgres_recovery":{"recovery_target_time_unix":target_unix,"recovery_target_action":"pause",
                                  "recovery_target_timeline":m["timeline"],"recovery_target_inclusive":True},
            "steps":["verify-independent-inventory-and-key-access", "restore-physical-base-with-restore-principal",
                     "replay-checksummed-WAL-to-target-and-pause", "validate-system-timeline-checkpoint-and-table-digests",
                     "validate-required-object-replicas", "prove-old-region-write-and-publish-fence",
                     "activate-new-epoch-with-durable-audit", "rebuild-outbox-consumers-and-cache",
                     "verify-read-reconciliation-before-traffic-cutover"],
            "traffic_cutover_allowed":False}

def verify_objects(root, objects):
    require(isinstance(objects,list) and 1<=len(objects)<=10000,"object inventory bound")
    root=Path(root)
    require(not root.is_symlink(),"symlink root rejected")
    rootfd=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    total=0
    try:
        for obj in objects:
            object_key(obj["key"]); digest(obj["sha256"]); integer(obj["size"],1,2**40)
            directory=os.dup(rootfd)
            try:
                parts=obj["key"].split("/")
                for part in parts[:-1]:
                    child=os.open(part,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,dir_fd=directory)
                    os.close(directory);directory=child
                fd=os.open(parts[-1],os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK,dir_fd=directory)
                with os.fdopen(fd,"rb") as f:
                    import stat
                    st=os.fstat(f.fileno())
                    require(stat.S_ISREG(st.st_mode),"non-regular object: "+obj["key"])
                    require(st.st_size==obj["size"],"object size mismatch: "+obj["key"])
                    h=hashlib.sha256();count=0
                    while chunk:=f.read(1024*1024):
                        count+=len(chunk);require(count<=obj["size"],"object changed during read")
                        h.update(chunk)
                    require(count==obj["size"] and h.hexdigest()==obj["sha256"],"object checksum mismatch: "+obj["key"])
            except FileNotFoundError as exc:
                raise Invalid("missing object: "+obj["key"]) from exc
            except OSError as exc:
                raise Invalid("unsafe or symlink object: "+obj["key"]) from exc
            finally:
                os.close(directory)
            total+=1
    finally:
        os.close(rootfd)
    return {"verified_objects":total,"healthy":True}

def raw_partition(records):
    require(isinstance(records,list) and 1 <= len(records) <= 10000,"raw partition bound")
    prior=None; ids=set(); blocks={}; transactions={}
    for r in records:
        exact(r,{"chain_id","height","transaction_index","log_index","block_hash","transaction_hash","payload_sha256"})
        require(type(r["chain_id"]) is int and r["chain_id"]==125,"wrong chain")
        for k in ("height","transaction_index","log_index"): integer(r[k],0,2**63-1)
        for k in ("block_hash","transaction_hash","payload_sha256"): digest(r[k])
        require(blocks.setdefault(r["height"],r["block_hash"])==r["block_hash"],"conflicting committed block hash")
        txpos=(r["height"],r["transaction_index"])
        require(transactions.setdefault(txpos,r["transaction_hash"])==r["transaction_hash"],"conflicting transaction position")
        position=(r["height"],r["transaction_index"],r["log_index"])
        require(prior is None or prior<position,"unordered or duplicate raw log")
        identity=(r["transaction_hash"],r["log_index"])
        require(identity not in ids,"raw event identity reused"); ids.add(identity); prior=position
    return {"sha256":sha(records),"count":len(records),"first_height":records[0]["height"],"last_height":records[-1]["height"]}

def reconcile(expected, actual):
    keys={"checkpoint","snapshot_id","counts","amounts_raw","event_identities_sha256"}
    for obj in (expected,actual):
        exact(obj,keys); checkpoint(obj["checkpoint"]); name(obj["snapshot_id"])
        require(isinstance(obj["counts"],dict) and 1<=len(obj["counts"])<=100,"count inventory bound")
        require(isinstance(obj["amounts_raw"],dict) and 1<=len(obj["amounts_raw"])<=100,"amount inventory bound")
        for k,v in obj["counts"].items(): name(k); integer(v)
        for k,v in obj["amounts_raw"].items():
            name(k); require(isinstance(v,str) and re.fullmatch(r"0|[1-9][0-9]{0,77}",v),"exact raw integer required")
        digest(obj["event_identities_sha256"])
    require(canonical(expected)==canonical(actual),"restoration reconciliation mismatch")
    return {"reconciled":True,"sha256":sha(actual)}

def measured_evidence(e):
    exact(e,{"version","requirement","deployment_sha256","source_sha256","inputs_sha256","inventory_sha256",
             "started_unix","ended_unix","failure_start_unix","failure_end_unix","metric","samples","outcome","limitations"})
    require(type(e["version"]) is int and e["version"]==1,"unsupported evidence")
    require(e["requirement"] in METRICS and e["metric"]==METRICS[e["requirement"]],"wrong metric")
    for k in ("deployment_sha256","source_sha256","inputs_sha256","inventory_sha256"): digest(e[k])
    integer(e["started_unix"],1); integer(e["ended_unix"],e["started_unix"])
    integer(e["failure_start_unix"],e["started_unix"],e["ended_unix"])
    integer(e["failure_end_unix"],e["failure_start_unix"],e["ended_unix"])
    require(e["outcome"] in {"pass","fail","blocked"},"invalid outcome")
    require(isinstance(e["limitations"],list) and len(e["limitations"])<=32 and
            all(isinstance(x,str) and 1<=len(x)<=1024 for x in e["limitations"]),"invalid limitations")
    require(isinstance(e["samples"],list) and 2<=len(e["samples"])<=10000,"metric timeline required")
    prior=-1
    for s in e["samples"]:
        exact(s,{"unix","value","service","environment","zone"})
        integer(s["unix"],e["started_unix"],e["ended_unix"]); integer(s["value"])
        require(s["unix"]>prior,"nonmonotonic sample");prior=s["unix"]
        for k in ("service","environment","zone"):name(s[k])
    return e


def resources(d):
    """Validate disabled operational declarations; never attest deployed behavior."""
    exact(d,{"version","activation","resources","iam","command_contracts"})
    require(type(d["version"]) is int and d["version"]==1,"unsupported resources")
    require(d["activation"]=="disabled-until-coordinator-review","declarations cannot activate recovery")
    groups=d["resources"]
    exact(groups,{"postgres","object_storage","broker","drills","cutover"})
    pg=groups["postgres"]
    exact(pg,{"archive_mode","archive_timeout","wal_level","base_backup_schedule","physical_backup_required","archive_command_contract","restore_command_contract"})
    require(pg["archive_mode"]=="on" and pg["archive_timeout"]=="60s" and pg["wal_level"]=="replica"
            and pg["base_backup_schedule"]=="0 2 * * *" and pg["physical_backup_required"] is True,"physical/WAL backup contract required")
    for key in ("archive_command_contract","restore_command_contract"):
        require(isinstance(pg[key],str) and 64<=len(pg[key])<=2048,"provider adapter contract missing")
    obj=groups["object_storage"]
    exact(obj,{"versioning","object_lock","encryption","replication","retention","legal_deletion"})
    require(obj["versioning"] is True and obj["object_lock"]=="compliance"
            and obj["encryption"]=="independent-region KMS keys"
            and obj["replication"]=="cross-region with distinct credentials","independent immutable encrypted replica contract required")
    exact(obj["retention"],{"daily_points","monthly_points","raw_archive_days"})
    require(obj["retention"]=={"daily_points":35,"monthly_points":12,"raw_archive_days":365}
            and all(type(v) is int for v in obj["retention"].values()),"retention review required")
    require(obj["legal_deletion"]=="separate reviewed policy; no automatic override","legal deletion policy review required")
    broker=groups["broker"]
    exact(broker,{"hot_backup_hours","rebuild_precedence","ack_contract"})
    require(type(broker["hot_backup_hours"]) is int and broker["hot_backup_hours"]==72,"hot retention must not replace archive")
    require(broker["rebuild_precedence"]==["verified stream backup for exact epoch","raw-chain immutable source for chain events","PostgreSQL business/outbox/inbox for application events"],"authoritative replay precedence required")
    require(broker["ack_contract"]=="Epoch-scoped credentials/subjects plus DB guard; old epoch acknowledgement must be rejected at broker authority, not merely local process.","broker epoch fencing required")
    drills=groups["drills"]
    exact(drills,{"isolated_restore_schedule","full_region_schedule","minimum_event_hours","data_classification","queries","capacity_qualified"})
    require(drills["isolated_restore_schedule"]=="monthly" and drills["full_region_schedule"]=="quarterly"
            and type(drills["minimum_event_hours"]) is int and drills["minimum_event_hours"]>=72
            and drills["minimum_event_hours"]<=8760 and drills["capacity_qualified"] is False,"unmeasured drill/capacity declaration")
    require(drills["data_classification"]=="synthetic or reviewed anonymized only","private data isolation required")
    require(drills["queries"]==["counts-by-table-at-fixed-checkpoint","integer-monetary-sums","stable-event-identity-digest","pending-outbox/inbox-reconciliation","missing-object-replicas"],"reconciliation query inventory required")
    cutover=groups["cutover"]
    exact(cutover,{"automatic_failback","requires","application_writes_before_activation"})
    require(cutover["automatic_failback"] is False and cutover["application_writes_before_activation"] is False,"unsafe dual-primary declaration")
    require(cutover["requires"]==["independent old-region write/publish fence proof","trusted manifest digest","physical/WAL restore validation","fixed checkpoint monetary/event reconciliation","all required object replicas","new durable epoch audit","verified recovery reads"],"cutover gates missing")
    expected={
     "application-runtime":([], ["backup:Read","backup:Write","backup:Delete","kms:Decrypt"]),
     "backup-writer":(["backup:PutImmutable","kms:Encrypt"],["backup:Delete","kms:Decrypt"]),
     "recovery-reader":(["backup:ReadPinnedVersion","kms:DecryptOwnEnvironment"],["backup:Write","backup:Delete","kms:DecryptOtherEnvironment"]),
     "retention-operator":(["backup:DeleteExpiredAfterLegalReview"],["backup:Overwrite","kms:Decrypt"])}
    exact(d["iam"],expected)
    for principal,(allow,deny) in expected.items():
        policy=d["iam"][principal];exact(policy,{"allow","deny"})
        require(policy["allow"]==allow and policy["deny"]==deny,"least-privilege policy drift: "+principal)
    cmd=d["command_contracts"]
    exact(cmd,{"base_backup","verify_base","restore","secrets"})
    require(cmd["base_backup"]==["pg_basebackup","--format=tar","--wal-method=stream","--checkpoint=spread","--manifest-checksums=SHA256","--no-password"],"unsafe base backup command")
    require(cmd["verify_base"]==["pg_verifybackup","--exit-on-error","<isolated-owned-backup-directory>"],"backup validation command required")
    require(cmd["restore"]==["pg_ctl","--pgdata=<isolated-owned-data-directory>","--wait","start"],"isolated restore command required")
    require(cmd["secrets"]=="External secret references and service-specific identities only; no passwords in argv/manifests.","secret isolation required")
    return d


def adversarial_scenario(requirement, result):
    """Validate native rehearsal results; configuration declarations are not results."""
    require(requirement in METRICS, 'unknown recovery adversary')
    require(isinstance(result, dict), 'native scenario result required')
    for key, value in result.items():
        if key.endswith('_sha256') or key in {'marker_before', 'marker_restored'}:
            digest(value); require(value != '0'*64, 'zero native state provenance')
    if requirement == 'D14_031':
        exact(result, {'backup_verified', 'paused', 'marker_before', 'marker_restored',
                       'marker_committed_unix', 'target_unix', 'deletion_unix',
                       'wal_archive_age_seconds', 'cross_region_lag_seconds'})
        require(result['backup_verified'] is True and result['paused'] is True, 'physical PITR not verified and paused')
        digest(result['marker_before']); digest(result['marker_restored'])
        require(result['marker_before'] != '0'*64 and result['marker_before'] == result['marker_restored'], 'committed marker lost')
        integer(result['marker_committed_unix'], 1)
        integer(result['target_unix'], result['marker_committed_unix'])
        integer(result['deletion_unix'], result['target_unix'] + 1)
        integer(result['wal_archive_age_seconds'], 0, 59)
        integer(result['cross_region_lag_seconds'], 0, 300)
    elif requirement == 'D14_032':
        exact(result, {'loss_unix', 'verified_read_unix', 'last_acknowledged_unix', 'last_restored_unix',
                       'expected_read_sha256', 'actual_read_sha256', 'writable_regions',
                       'capacity_samples', 'primary_principal', 'recovery_principal'})
        integer(result['loss_unix'], 1)
        integer(result['verified_read_unix'], result['loss_unix'], result['loss_unix'] + 1800)
        integer(result['last_acknowledged_unix'], 1, result['loss_unix'])
        integer(result['last_restored_unix'], max(1, result['last_acknowledged_unix'] - 300), result['last_acknowledged_unix'])
        for key in ('expected_read_sha256', 'actual_read_sha256'): digest(result[key])
        require(result['expected_read_sha256'] != '0'*64 and result['actual_read_sha256'] == result['expected_read_sha256'], 'recovery reads differ')
        integer(result['writable_regions'], 1, 1)
        require(isinstance(result['capacity_samples'], list) and 2 <= len(result['capacity_samples']) <= 10000, 'absent measured recovery capacity')
        prior = -1
        for sample in result['capacity_samples']:
            exact(sample, {'unix', 'verified_reads', 'failed_reads'})
            integer(sample['unix'], result['loss_unix'], result['verified_read_unix'])
            require(sample['unix'] > prior, 'nonmonotonic capacity sample'); prior = sample['unix']
            integer(sample['verified_reads'], 1); integer(sample['failed_reads'], 0, 0)
        for key in ('primary_principal', 'recovery_principal'): name(result[key])
        require(result['primary_principal'] != result['recovery_principal'], 'shared regional recovery credential')
    elif requirement == 'D14_033':
        exact(result, {'stale_write_sqlstate', 'stale_ack_error', 'before_sha256', 'after_sha256', 'epoch_before', 'epoch_after'})
        require(result['stale_write_sqlstate'] == '55000' and result['stale_ack_error'] in {'authorization-violation', 'authentication-revoked'}, 'stale actor not rejected by authority')
        for key in ('before_sha256', 'after_sha256'): digest(result[key])
        require(result['before_sha256'] == result['after_sha256'], 'stale actor mutated recovered state')
        integer(result['epoch_before'], 1); integer(result['epoch_after'], result['epoch_before'] + 1, result['epoch_before'] + 1)
    elif requirement == 'D14_034':
        exact(result, {'pending_before', 'pending_after', 'republished_event_id', 'original_event_id',
                       'effects_before', 'effects_after', 'inbox_before', 'inbox_after', 'delivery_attempts'})
        integer(result['pending_before'], 1); integer(result['pending_after'], 0, 0)
        require(isinstance(result['original_event_id'], str) and 1 <= len(result['original_event_id']) <= 512 and
                result['republished_event_id'] == result['original_event_id'], 'unpublished event identity changed')
        integer(result['effects_before'], 1); integer(result['effects_after'], result['effects_before'], result['effects_before'])
        integer(result['inbox_before'], 1); integer(result['inbox_after'], result['inbox_before'], result['inbox_before'])
        integer(result['delivery_attempts'], 2)
    elif requirement == 'D14_035':
        exact(result, {'disks_removed', 'source', 'target_watermark', 'consumer_watermark', 'expected_identities_sha256',
                       'actual_identities_sha256', 'replayed_events', 'fabricated_events', 'old_epoch_events'})
        integer(result['disks_removed'], 1)
        require(result['source'] in {'verified-hot-stream', 'raw-chain-and-postgresql-outbox'}, 'unverified recovery source')
        integer(result['target_watermark'], 1); integer(result['consumer_watermark'], result['target_watermark'], result['target_watermark'])
        for key in ('expected_identities_sha256', 'actual_identities_sha256'): digest(result[key])
        require(result['expected_identities_sha256'] != '0'*64 and result['expected_identities_sha256'] == result['actual_identities_sha256'], 'replay changed raw identity')
        integer(result['replayed_events'], 1); integer(result['fabricated_events'], 0, 0); integer(result['old_epoch_events'], 0, 0)
    elif requirement == 'D14_036':
        exact(result, {'object_key', 'reported_object_key', 'partition_halted', 'expected_sha256', 'corrupt_sha256',
                       'replacement_sha256', 'independent_principal', 'archive_principal', 'effects_before', 'effects_after'})
        object_key(result['object_key']); require(result['object_key'] == result['reported_object_key'], 'corrupt object not identified')
        require(result['partition_halted'] is True, 'corrupt partition continued')
        for key in ('expected_sha256', 'corrupt_sha256', 'replacement_sha256'): digest(result[key])
        require(result['corrupt_sha256'] != result['expected_sha256'] and result['replacement_sha256'] == result['expected_sha256'], 'independent replacement not verified')
        for key in ('independent_principal', 'archive_principal'): name(result[key])
        require(result['independent_principal'] != result['archive_principal'], 'independent recovery source required')
        integer(result['effects_before']); integer(result['effects_after'], result['effects_before'], result['effects_before'])
    elif requirement == 'D14_037':
        exact(result, {'dataset_first_unix', 'dataset_last_unix', 'classification', 'fixed_watermark', 'restored_watermark',
                       'expected_counts', 'actual_counts', 'expected_amounts_raw', 'actual_amounts_raw',
                       'expected_identities_sha256', 'actual_identities_sha256'})
        integer(result['dataset_first_unix'], 1); integer(result['dataset_last_unix'], result['dataset_first_unix'] + 72*3600)
        require(result['classification'] in {'synthetic', 'reviewed-anonymized'}, 'unreviewed private drill data')
        integer(result['fixed_watermark'], 1); integer(result['restored_watermark'], result['fixed_watermark'], result['fixed_watermark'])
        for prefix in ('expected', 'actual'):
            counts, amounts = result[prefix+'_counts'], result[prefix+'_amounts_raw']
            require(isinstance(counts, dict) and 1 <= len(counts) <= 100, 'missing identity counts')
            require(isinstance(amounts, dict) and 1 <= len(amounts) <= 100, 'missing monetary aggregates')
            for key, value in counts.items(): name(key); integer(value)
            for key, value in amounts.items():
                name(key); require(isinstance(value, str) and re.fullmatch(r'0|-?[1-9][0-9]{0,77}', value), 'exact signed raw monetary amount required')
            digest(result[prefix+'_identities_sha256'])
        require(result['expected_counts'] == result['actual_counts'] and
                result['expected_amounts_raw'] == result['actual_amounts_raw'] and
                result['expected_identities_sha256'] == result['actual_identities_sha256'], 'fixed watermark reconciliation differs')
    elif requirement == 'D14_038':
        exact(result, {'principal', 'delete_audit_principal', 'decrypt_audit_principal', 'delete_error', 'decrypt_error',
                       'object_before_sha256', 'object_after_sha256', 'plaintext_bytes', 'delete_request_id', 'decrypt_request_id'})
        name(result['principal'])
        require(result['delete_audit_principal'] == result['principal'] == result['decrypt_audit_principal'], 'denial audit actor mismatch')
        require(result['delete_error'] == 'AccessDenied' and result['decrypt_error'] == 'AccessDeniedException', 'provider permission denial required')
        for key in ('object_before_sha256', 'object_after_sha256'): digest(result[key])
        require(result['object_before_sha256'] == result['object_after_sha256'], 'denied application deleted backup')
        integer(result['plaintext_bytes'], 0, 0)
        for key in ('delete_request_id', 'decrypt_request_id'):
            require(isinstance(result[key], str) and re.fullmatch(r'[A-Za-z0-9-]{8,128}', result[key]), 'actual provider denial request identity required')
    elif requirement == 'D14_039':
        exact(result, {'missing_before', 'missing_after', 'missing_keys', 'false_healthy', 'restored_keys',
                       'explicitly_unavailable_keys', 'replication_lag_seconds'})
        integer(result['missing_before'], 1); integer(result['missing_after'], 0, 0)
        require(result['false_healthy'] is False, 'missing replica reported healthy')
        for key in ('missing_keys', 'restored_keys', 'explicitly_unavailable_keys'):
            require(isinstance(result[key], list) and len(result[key]) <= 10000, 'object recovery inventory bound')
            for item in result[key]: object_key(item)
            require(len(set(result[key])) == len(result[key]), 'duplicate recovered object')
        require(len(result['missing_keys']) == result['missing_before'] and
                set(result['missing_keys']) == set(result['restored_keys']) | set(result['explicitly_unavailable_keys']) and
                not set(result['restored_keys']) & set(result['explicitly_unavailable_keys']), 'missing objects unaccounted')
        integer(result['replication_lag_seconds'], 0, 300)
    else:
        exact(result, {'connectivity_restored_unix', 'old_primary_read_only', 'old_primary_write_sqlstate',
                       'old_primary_publish_error', 'writable_regions', 'epoch_before_return', 'epoch_after_return',
                       'before_sha256', 'after_sha256', 'automatic_failback'})
        integer(result['connectivity_restored_unix'], 1)
        require(result['old_primary_read_only'] is True and result['old_primary_write_sqlstate'] in {'25006', '55000', '42501'} and
                result['old_primary_publish_error'] in {'authorization-violation', 'authentication-revoked'}, 'old primary resumed authority')
        integer(result['writable_regions'], 1, 1); integer(result['epoch_before_return'], 1)
        integer(result['epoch_after_return'], result['epoch_before_return'], result['epoch_before_return'])
        for key in ('before_sha256', 'after_sha256'): digest(result[key])
        require(result['before_sha256'] == result['after_sha256'] and result['automatic_failback'] is False, 'implicit failback changed authority')
    return result


def release_timeline(rows, *, identity, run_id, bindings, started_ns, ended_ns, step_seconds, authority_sha256):
    from decimal import Decimal, InvalidOperation
    require(isinstance(rows, list) and len(rows) >= 2, 'measured recovery metric timeline absent')
    integer(started_ns, 1, 2**100); integer(ended_ns, started_ns + 1, 2**100)
    integer(step_seconds, 1, 60); digest(authority_sha256)
    expected_samples = (ended_ns-started_ns)//(step_seconds*10**9)
    require(max(2, expected_samples) <= len(rows) <= expected_samples+3, 'recovery cadence sample count incomplete or excessive')
    exact(bindings, {'candidate', 'interface', 'topology', 'generator'})
    for value in bindings.values(): digest(value); require(value != '0'*64, 'zero shared capture binding')
    require(bindings['candidate'] == identity['source_sha256'] and bindings['interface'] == identity['interface_sha256'],
            'shared capture recovery candidate differs')
    samples = {rid: [] for rid in METRICS}
    prior = None
    for row in rows:
        exact(row, {'version', 'lane', 'run_id', 'bindings', 'identity', 'kind', 'observed_utc_ns', 'payload'})
        require(type(row['version']) is int and row['version'] == 4 and row['lane'] == 'L44' and
                row['run_id'] == run_id and row['bindings'] == bindings and row['identity'] == identity and row['kind'] == 'metrics',
                'foreign recovery timeline')
        stamp = integer(row['observed_utc_ns'], started_ns, ended_ns, )
        if prior is not None: require(0 < stamp - prior <= (step_seconds + 5)*10**9, 'recovery capture gap, overlap or reorder')
        prior = stamp
        payload = row['payload']; exact(payload, {'authority_sha256', 'queries'})
        require(payload['authority_sha256'] == authority_sha256, 'metric authority changed')
        exact(payload['queries'], METRICS)
        for rid, response in payload['queries'].items():
            require(isinstance(response, dict) and response.get('status') == 'success' and not response.get('warnings'),
                    'failed or warned native recovery metric query')
            data = response.get('data', {})
            require(data.get('resultType') == 'vector' and isinstance(data.get('result'), list) and len(data['result']) == 1,
                    'missing or ambiguous recovery metric: ' + rid)
            result = data['result'][0]; require(isinstance(result, dict), 'invalid native recovery sample')
            labels = result.get('metric', {})
            require(labels.get('service') == identity['service'] and labels.get('environment') == identity['environment'] and
                    labels.get('zone') in {identity['primary_region'], identity['recovery_region']}, 'foreign recovery metric labels')
            value = result.get('value')
            require(isinstance(value, list) and len(value) == 2, 'native metric value absent')
            try:
                timestamp, numeric = Decimal(str(value[0])), Decimal(value[1])
            except (InvalidOperation, ValueError, TypeError) as exc:
                raise Invalid('invalid numeric recovery sample') from exc
            require(timestamp.is_finite() and numeric.is_finite() and numeric >= 0 and
                    abs(timestamp*10**9 - stamp) <= 5*10**9, 'stale, future or invalid recovery metric')
            if rid in {'D14_033', 'D14_034', 'D14_035', 'D14_036', 'D14_037', 'D14_038', 'D14_039', 'D14_040'}:
                require(numeric == numeric.to_integral_value(), 'fractional recovery count, timestamp or epoch')
            if samples[rid]: require(samples[rid][-1]['labels'] == labels, 'recovery native series identity changed')
            samples[rid].append({'unix_ns': stamp, 'value': str(numeric), 'source_timestamp': str(timestamp), 'labels': labels})
    require(rows[0]['observed_utc_ns'] - started_ns <= (step_seconds+5)*10**9 and
            ended_ns - rows[-1]['observed_utc_ns'] <= (step_seconds+5)*10**9, 'incomplete recovery capture endpoints')
    return samples


def release_metric_checks(samples, *, ended_ns):
    from decimal import Decimal
    exact(samples, METRICS)
    checks = {}
    counters = {'D14_033', 'D14_035', 'D14_036', 'D14_038'}
    for rid, timeline in samples.items():
        require(isinstance(timeline, list) and len(timeline) >= 2, 'missing measured requirement samples')
        values = [Decimal(item['value']) for item in timeline]
        require(all(value.is_finite() and value >= 0 for value in values), 'nonfinite or negative recovery metric')
        passed = True; reason = 'measured_timeline'
        if rid in counters:
            passed = all(b >= a for a, b in zip(values, values[1:])) and values[-1] > values[0]
            reason = 'counter_reset_or_required_injection_absent' if not passed else reason
        elif rid == 'D14_031': passed = max(values) < 60; reason = 'wal_archive_age'
        elif rid == 'D14_032': passed = max(values) <= 1800; reason = 'region_restore_elapsed'
        elif rid == 'D14_034': passed = values[-1] == 0; reason = 'pending_outbox'
        elif rid == 'D14_037': passed = values[-1] > 0 and values[-1]*10**9 <= ended_ns and ended_ns-values[-1]*10**9 <= 300*10**9; reason = 'fresh_restore_drill'
        elif rid == 'D14_039': passed = max(values) > 0 and values[-1] == 0; reason = 'missing_object_recovery'
        else: passed = all(value == value.to_integral_value() for value in values) and values[-1] > values[0] and all(b >= a for a,b in zip(values,values[1:])); reason = 'monotonic_recovery_epoch'
        checks[rid] = {'passed': passed, 'reason': reason, 'first': str(values[0]), 'last': str(values[-1]),
                       'maximum': str(max(values)), 'samples': len(values)}
    return checks
