#!/usr/bin/env python3
"""Validate frozen capability evidence without contacting providers or chains."""
import argparse
import hashlib
import json
import re
from datetime import datetime
from pathlib import Path

KINDS = {"source", "deployment", "provider", "controller", "qualification"}
STATES = {"verified", "present_unverified", "absent", "blocked"}
REQUIRED_IDS = {"core_market", "account_profile", "social", "media_live", "funding", "conditional_orders", "automation", "bounty", "rewards", "push", "voice", "operator", "realtime", "external_wallet"}
ARTIFACTS = {"spec/production-system/integration/contracts/v1/" + name for name in ("schema-definitions.json", "openapi.json", "asyncapi.json", "catalog-map.json", "chain/abi-catalog.json", "chain/event-authorities.json", "chain/golden-fixtures.json", "chain/mainnet-attestation.json", "chain/files-manifest.json")}
RUNTIME_CHECKS = {
    "provider": {"jwt_trust", "subject_isolation", "consent_enforced", "revocation", "chain125", "actual_provider_delivery"},
    "controller": {"registered_handler", "authorization", "durable_idempotency", "failure_recovery", "real_dependency_effect"},
    "deployment": {"chain125", "code_identity", "roles", "configuration", "ownership_acceptance"},
    "qualification": {"actual_user_journey", "negative_paths", "dependency_effects", "no_unavailable_success"},
}


def load(path):
    return json.loads(path.read_text())


def epoch(value):
    return float(value) if isinstance(value, (int, float)) else datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()


def proof_errors(proof, kind, capability, digest, not_before):
    errors = []
    if not isinstance(proof, dict) or proof.get("kind") != kind or proof.get("capabilityId") != capability:
        return ["runtime proof kind/scope mismatch"]
    checks = proof.get("checks")
    if proof.get("outcome") != "passed" or proof.get("exitCode") != 0 or not proof.get("command") or not isinstance(checks, dict) or not RUNTIME_CHECKS.get(kind, set()) <= set(checks) or not checks or any(v is not True for v in checks.values()):
        errors.append("runtime proof lacks passed meaningful checks")
    if proof.get("sourceDigest") != digest or not_before is None or epoch(proof.get("observedAt", "1970-01-01T00:00:00Z")) < not_before:
        errors.append("runtime proof stale or source digest mismatch")
    if proof.get("statusCodesOnly") is not False:
        errors.append("status-code-only proof cannot qualify capability")
    return errors


def catalog_errors(definitions, catalog, api, schemas, endpoints):
    errors = []
    if set(catalog.get("schemas", {})) != schemas or set(catalog.get("endpoints", {})) != endpoints:
        errors.append("materialized catalog ID coverage mismatch")
    for sid, item in catalog.get("schemas", {}).items():
        name = item.get("wireName")
        if name not in definitions or not definitions[name] or item.get("ref") != f"schema-definitions.json#/$defs/{name}":
            errors.append(f"{sid}: catalog schema definition missing or empty")
    for eid, item in catalog.get("endpoints", {}).items():
        if any(item.get(k) not in definitions or not definitions[item[k]] for k in ("request", "data", "response")):
            errors.append(f"{eid}: named endpoint definition missing or empty")
        operation = api.get("paths", {}).get(item.get("path"), {}).get(str(item.get("method", "")).lower(), {})
        if operation.get("operationId") != eid:
            errors.append(f"{eid}: OpenAPI operation mismatch")
    return errors


def validate(document, root, spec, authorities, require_materialized=True, not_before=None, bundle=None, chain_bundle=None, catalog_input=None):
    bundle=(bundle or root / "launchpad/spec/production-system/integration/contracts/v1").resolve()
    chain_bundle=(chain_bundle or root / "launchpad/spec/production-system/integration/contracts/v1").resolve()
    if not bundle.is_relative_to(root.resolve()) or not chain_bundle.is_relative_to(root.resolve()):return ["contract bundles must belong to source root"]
    required_artifacts={str((bundle/name).relative_to(root.resolve())) for name in ("schema-definitions.json","openapi.json","asyncapi.json","catalog-map.json")} | {str((chain_bundle/"chain"/name).relative_to(root.resolve())) for name in ("abi-catalog.json","event-authorities.json","golden-fixtures.json","mainnet-attestation.json","files-manifest.json")}
    errors = []
    if catalog_input is None:
        text = spec.read_text()
        endpoints = set(re.findall(r"^\[endpoint\.([^\]]+)\]", text, re.M))
        schemas = set(re.findall(r"^\[schema\.([^\]]+)\]", text, re.M))
        if len(endpoints) != 108 or len(schemas) != 59:
            errors.append("catalog must contain exactly 108 endpoints and 59 schemas")
        if document.get("catalog") != {"endpointCount": 108, "schemaCount": 59}:
            errors.append("catalog counts differ")
    else:
        catalog_input=catalog_input.resolve()
        if not catalog_input.is_relative_to(bundle):return ["selected catalog must belong to configured bundle"]
        selected=load(catalog_input)
        selected=selected.get("sections",selected)
        endpoints=set(selected["endpoint"]);schemas=set(selected["schema"])
        if not endpoints or not schemas or document.get("catalog")!={"endpointCount":len(endpoints),"schemaCount":len(schemas)}:
            errors.append("catalog counts differ from explicit selected source")
        if document.get("catalogInputSha256")!=hashlib.sha256(catalog_input.read_bytes()).hexdigest():
            errors.append("selected catalog source digest mismatch")
    if not required_artifacts <= set(document.get("requiredArtifacts", [])):
        errors.append("required artifact set removed")
    wallet = document.get("wallet", {})
    if wallet.get("mode") != "external_web3" or wallet.get("custody") != "wallet_owned_keys":
        errors.append("core onboarding must use external wallet-owned keys")
    if wallet.get("walletConnect", {}).get("requiredForCore") is not False:
        errors.append("WalletConnect cannot be a mandatory core onboarding dependency")
    if "provider" in document:
        errors.append("superseded embedded provider cannot remain active")
    source_ids = set()
    for source in document.get("sources", []):
        sid = source.get("id")
        if not isinstance(sid, str) or sid in source_ids:
            errors.append("duplicate or invalid source identity")
        source_ids.add(sid)
        if source.get("proofKind") != "source":
            errors.append(f"{sid}: source record cannot impersonate runtime proof")
        if source.get("mode") == "frozen_provenance":
            record = source.get("record")
            if not isinstance(record, dict) or not record.get("facts") or not re.fullmatch(r"[a-f0-9]{64}", record.get("sourceSha256", "")):
                errors.append(f"{sid}: malformed frozen provenance")
            if hashlib.sha256(json.dumps(record, sort_keys=True, separators=(",", ":")).encode()).hexdigest() != source.get("recordSha256"):
                errors.append(f"{sid}: stale frozen provenance hash")
            if require_materialized and isinstance(record, dict) and record.get("implementationPath"):
                implementation = (root / record["implementationPath"]).resolve()
                if not implementation.is_relative_to(root.resolve()) or not implementation.is_file():
                    errors.append(f"{sid}: admitted source implementation absent")
                elif hashlib.sha256(implementation.read_bytes()).hexdigest() != record.get("sourceSha256"):
                    errors.append(f"{sid}: admitted source implementation hash mismatch")
            continue
        authority = authorities.get(source.get("authority"))
        rel = source.get("path", "")
        if authority is None or not isinstance(rel, str):
            errors.append(f"{sid}: unknown source authority")
            continue
        path = (authority / rel).resolve()
        if not path.is_relative_to(authority.resolve()):
            errors.append(f"{sid}: source escapes authority")
        elif not path.is_file():
            errors.append(f"{sid}: missing source {path}")
        elif hashlib.sha256(path.read_bytes()).hexdigest() != source.get("sha256"):
            errors.append(f"{sid}: stale source hash")
    covered = set()
    covered_schemas = set()
    ids = set()
    digest = hashlib.sha256(json.dumps(document.get("sources", []), sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    for cap in document.get("capabilities", []):
        cid = cap.get("id")
        if not isinstance(cid, str) or cid in ids:
            errors.append("duplicate or invalid capability ID")
        ids.add(cid)
        if cap.get("state") not in {"blocked", "ready"} or cap.get("requiredForRelease") is not True:
            errors.append(f"{cid}: invalid state/required flag")
        refs = cap.get("endpointRefs", [])
        if not isinstance(refs, list) or not set(refs) <= endpoints:
            errors.append(f"{cid}: invalid endpoint references")
        else:
            covered.update(refs)
        if not set(cap.get("schemaRefs", [])) <= schemas:
            errors.append(f"{cid}: invalid schema references")
        covered_schemas.update(cap.get("schemaRefs", []))
        prerequisites = cap.get("prerequisites", [])
        if cid == "external_wallet" and any(p.get("kind") == "provider" for p in prerequisites):
            errors.append("external_wallet: hosted custody provider is not a core prerequisite")
        if not prerequisites:
            errors.append(f"{cid}: missing typed prerequisites")
        kinds = set()
        all_verified = True
        for pre in prerequisites:
            kinds.add(pre.get("kind"))
            if pre.get("kind") not in KINDS or pre.get("state") not in STATES:
                errors.append(f"{cid}: invalid prerequisite type/state")
            evidence = pre.get("sourceRefs", [])
            if not isinstance(evidence, list) or not set(evidence) <= source_ids:
                errors.append(f"{cid}: invalid prerequisite source references")
            if pre.get("state") == "verified" and not evidence:
                errors.append(f"{cid}: verified prerequisite lacks evidence")
            if pre.get("state") == "verified" and pre.get("kind") != "source":
                proof_path = pre.get("proofPath")
                evidence_root = authorities.get("evidence")
                if not isinstance(proof_path, str) or evidence_root is None:
                    errors.append(f"{cid}: runtime prerequisite missing fresh proof file")
                else:
                    target = (evidence_root / proof_path).resolve()
                    if not target.is_relative_to(evidence_root.resolve()) or not target.is_file():
                        errors.append(f"{cid}: runtime proof absent or escapes evidence root")
                    else:
                        if not_before is None or target.stat().st_mtime < not_before:
                            errors.append(f"{cid}: runtime proof file predates run")
                        errors.extend(f"{cid}: {e}" for e in proof_errors(load(target), pre["kind"], cid, digest, not_before))
            all_verified &= pre.get("state") == "verified"
        if "qualification" not in kinds:
            errors.append(f"{cid}: missing qualification prerequisite")
        if cap.get("state") == "ready" and (not all_verified or not cap.get("qualificationEvidence")):
            errors.append(f"{cid}: ready without verified prerequisites and qualification")
        if cap.get("requiredForRelease") and cap.get("state") != "ready" and not cap.get("releaseBlockers"):
            errors.append(f"{cid}: missing release blocker")
        if cap.get("state") == "ready":
            q = cap.get("qualificationEvidence") or {}
            if q.get("outcome") != "passed" or q.get("statusCodesOnly") is not False or not set(q.get("sourceRefs", [])) <= source_ids or not q.get("sourceRefs"):
                errors.append(f"{cid}: unavailable HTTP response is not qualification")
            if q.get("kind") != "qualification" or not q.get("proofPath"):
                errors.append(f"{cid}: source-only qualification evidence forbidden")
            errors.extend(f"{cid}: {e}" for e in proof_errors(q, "qualification", cid, digest, not_before))
            if isinstance(q.get("proofPath"), str) and authorities.get("evidence"):
                base = authorities["evidence"].resolve()
                proof_file = (base / q["proofPath"]).resolve()
                if not proof_file.is_relative_to(base) or not proof_file.is_file():
                    errors.append(f"{cid}: qualification proof file missing")
                elif load(proof_file) != {k: v for k, v in q.items() if k != "proofPath"}:
                    errors.append(f"{cid}: qualification reference contradicts proof file")
            if cid == "conditional_orders" and not document.get("chain", {}).get("optionalPECORManifest"):
                errors.append("conditional_orders: optional deployment absent")
            if cid == "external_wallet" and document.get("wallet", {}).get("liveBrowserQualified") is not True:
                errors.append("external_wallet: browser connection/authentication not qualified")
            if cid == "core_market" and document.get("chain", {}).get("status") != "accepted":
                errors.append("core_market: deployment acceptance incomplete")
    if ids != REQUIRED_IDS:
        errors.append("required capability set missing or changed without version review")
    if covered != endpoints:
        errors.append("not every required endpoint has a capability")
    if covered_schemas != schemas:
        errors.append("not every required schema has a capability reference")
    if require_materialized:
        for rel in document.get("requiredArtifacts", []):
            path = (root / rel).resolve()
            if not path.is_relative_to(root.resolve()) or not path.is_file():
                errors.append(f"missing required frozen artifact: {rel}")
            elif not isinstance(load(path), dict) or not load(path):
                errors.append(f"invalid frozen JSON object: {rel}")
        schema_path = bundle / "schema-definitions.json"
        catalog_path = bundle / "catalog-map.json"
        openapi_path = bundle / "openapi.json"
        if schema_path.is_file() and catalog_path.is_file() and openapi_path.is_file():
            definitions = load(schema_path).get("$defs", {})
            catalog = load(catalog_path)
            api = load(openapi_path)
            errors.extend(catalog_errors(definitions, catalog, api, schemas, endpoints))
        attestation = chain_bundle / "chain/mainnet-attestation.json"
        if attestation.is_file():
            observed = load(attestation)
            if str(observed.get("chainId")) != "125" or observed.get("configuration", {}).get("creationFee") != "1000000" or str(observed.get("usdlDecimals")) != "6":
                errors.append("core attestation denomination/chain identity mismatch")
            if not observed.get("beacon", {}).get("ownershipComplete") and document.get("chain", {}).get("status") == "accepted":
                errors.append("core attestation contradicts accepted beacon status")
        evidence_root = authorities.get("evidence")
        fresh = evidence_root / "chain/mainnet-attestation.json" if evidence_root else None
        if fresh is None or not fresh.is_file() or not_before is None:
            errors.append("fresh run chain attestation and --not-before required")
        else:
            observation = load(fresh)
            if fresh.stat().st_mtime < not_before or epoch(observation.get("observedAt", "1970-01-01T00:00:00Z")) < not_before:
                errors.append("fresh chain attestation predates qualification run")
            if str(observation.get("chainId")) != "125" or observation.get("configuration", {}).get("creationFee") != "1000000" or str(observation.get("usdlDecimals")) != "6" or not observation.get("identities") or not observation.get("block", {}).get("hash"):
                errors.append("fresh chain attestation identity missing or mismatched")
    return errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--check", action="store_true", required=True)
    parser.add_argument("--root", type=Path, default=Path.cwd())
    parser.add_argument("--spec", type=Path)
    parser.add_argument("--contracts-root", type=Path)
    parser.add_argument("--bundle", type=Path)
    parser.add_argument("--catalog", type=Path)
    parser.add_argument("--chain-bundle", type=Path)
    parser.add_argument("--evidence-root", type=Path, required=True)
    parser.add_argument("--not-before", required=True)
    parser.add_argument("--report", type=Path)
    args = parser.parse_args()
    root = args.root.resolve()
    try:
        bundle=(args.bundle or root / "launchpad/spec/production-system/integration/contracts/v1").resolve()
        chain_bundle=(args.chain_bundle or root / "launchpad/spec/production-system/integration/contracts/v1").resolve()
        document = load(bundle / "capabilities.json")
        errors = validate(document, root, args.spec or root / "launchpad/spec/production-system/spec.kvx", {"workspace": root, "contracts": args.contracts_root or root / "contracts/launchpad", "evidence": args.evidence_root}, not_before=epoch(args.not_before),bundle=bundle,chain_bundle=chain_bundle,catalog_input=args.catalog)
    except (OSError, ValueError, TypeError, KeyError, AttributeError) as exc:
        errors = [f"invalid or missing frozen input: {exc}"]
    result = {"coverage": "interface:capability-prerequisites", "passed": not errors, "errors": errors, "releaseQualified": False, "note": "Interface consistency does not qualify blocked product capabilities."}
    if args.report:
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))
    return bool(errors)


if __name__ == "__main__":
    raise SystemExit(main())
