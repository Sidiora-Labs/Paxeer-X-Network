use super::*;

fn capture() -> Result<(serde_json::Value, SequencerAuthorization), String> {
    let path = std::path::Path::new(env!("CARGO_MANIFEST_DIR"))
        .join("tests/fixtures/maintained-authority.json");
    let value: serde_json::Value =
        serde_json::from_slice(&std::fs::read(path).map_err(|error| error.to_string())?)
            .map_err(|error| error.to_string())?;
    let authority = SequencerAuthorization::new(
        parse_hex32(text(&value, "sequencer_id")?).map_err(|_| "sequencer identity")?,
        parse_hex32(text(&value, "sequencer_public_key")?).map_err(|_| "sequencer key")?,
        text(&value, "first_batch")?
            .parse()
            .map_err(|_| "first batch")?,
        text(&value, "last_batch")?
            .parse()
            .map_err(|_| "last batch")?,
    );
    Ok((value, authority))
}

fn text<'a>(value: &'a serde_json::Value, field: &str) -> Result<&'a str, String> {
    value[field]
        .as_str()
        .ok_or_else(|| format!("captured field missing: {field}"))
}

fn envelope(capture: &serde_json::Value, maintenance: bool) -> Result<serde_json::Value, String> {
    let evidence = &capture["authority"]["batch_evidence"];
    let identity = &evidence["batch_identity"];
    let bytes = if maintenance {
        text(identity, "receipt_hex")?
    } else {
        text(capture, "receipt_hex")?
    };
    let receipt = decode_hex(bytes, MAX_RECEIPT_BYTES).map_err(|_| "captured receipt encoding")?;
    let header_bytes = decode_hex(text(evidence, "header_hex")?, MAX_HEADER_BYTES)
        .map_err(|_| "captured header encoding")?;
    let header = layerx_wire::receipt::decode_batch_header(&header_bytes)
        .map_err(|error| format!("{error:?}"))?;
    let (sequence, timestamp, root) =
        head_claims(&receipt, &header).map_err(|_| "captured head claims")?;
    let digest = if maintenance {
        Sha256::digest(&receipt).into()
    } else {
        receipt_digest(&receipt).map_err(|error| format!("{error:?}"))?
    };
    Ok(serde_json::json!({
        "current": true, "receipt_hex": bytes, "receipt_digest": hex(&digest), "state_root":hex(&root),
        "observed_sequence":sequence, "observed_at":timestamp,
        "batch_evidence": {"header_hex": text(evidence,"header_hex")?,
            "header_signature":text(evidence,"header_signature")?,
            "receipt_proof_hex":if maintenance {text(identity,"receipt_proof_hex")?} else {text(evidence,"receipt_proof_hex")?},
            "batch_identity": identity}
    }))
}

fn verified(
    value: serde_json::Value,
    authority: &SequencerAuthorization,
) -> Result<VerifiedState, String> {
    let head: HeadDocument = serde_json::from_value(value).map_err(|error| error.to_string())?;
    verified_state(&head, authority).map_err(|_| "production state verification refused".to_owned())
}

#[test]
fn genuine_native_maintenance_and_ordinary_digests_are_preserved() -> Result<(), String> {
    let (capture, authority) = capture()?;
    let head = envelope(&capture, true)?;
    let state = verified(head.clone(), &authority)?;
    let raw = decode_hex(text(&head, "receipt_hex")?, MAX_RECEIPT_BYTES)
        .map_err(|_| "maintenance receipt")?;
    assert_eq!(state.receipt_digest, <[u8; 32]>::from(Sha256::digest(&raw)));
    assert_ne!(
        state.receipt_digest,
        receipt_digest(&raw).map_err(|error| format!("{error:?}"))?
    );
    let served = document(&state);
    assert_eq!(served["state_root"], head["state_root"]);
    assert_eq!(served["observed_sequence"], head["observed_sequence"]);
    assert_eq!(served["timestamp_ms"], head["observed_at"]);
    assert_eq!(served["receipt_digest"], head["receipt_digest"]);
    println!("GATEWAY_MAINTENANCE_CASE native-maintenance");
    let ordinary = envelope(&capture, false)?;
    let ordinary_state = verified(ordinary.clone(), &authority)?;
    let raw = decode_hex(text(&ordinary, "receipt_hex")?, MAX_RECEIPT_BYTES)
        .map_err(|_| "ordinary receipt")?;
    assert_eq!(
        ordinary_state.receipt_digest,
        receipt_digest(&raw).map_err(|error| format!("{error:?}"))?
    );
    println!("GATEWAY_MAINTENANCE_CASE ordinary-domain-preserved");
    Ok(())
}

#[test]
fn digest_domain_confusion_is_refused_both_ways() -> Result<(), String> {
    let (capture, authority) = capture()?;
    for maintenance in [true, false] {
        let mut head = envelope(&capture, maintenance)?;
        let raw =
            decode_hex(text(&head, "receipt_hex")?, MAX_RECEIPT_BYTES).map_err(|_| "receipt")?;
        let wrong = if maintenance {
            receipt_digest(&raw).map_err(|error| format!("{error:?}"))?
        } else {
            Sha256::digest(&raw).into()
        };
        head["receipt_digest"] = serde_json::json!(hex(&wrong));
        assert!(verified(head, &authority).is_err());
        println!("GATEWAY_MAINTENANCE_CASE domain-confusion-{maintenance}");
    }
    Ok(())
}

#[test]
fn real_maintenance_evidence_tamper_and_foreign_authority_are_refused() -> Result<(), String> {
    let (capture, authority) = capture()?;
    let head = envelope(&capture, true)?;
    for pointer in [
        "/receipt_hex",
        "/receipt_digest",
        "/state_root",
        "/batch_evidence/header_hex",
        "/batch_evidence/header_signature",
        "/batch_evidence/receipt_proof_hex",
    ] {
        let mut changed = head.clone();
        let value = changed
            .pointer(pointer)
            .and_then(serde_json::Value::as_str)
            .ok_or("evidence missing")?;
        let mut raw = decode_hex(value, MAX_RECEIPT_BYTES).map_err(|_| "tamper encoding")?;
        *raw.last_mut().ok_or("empty evidence")? ^= 1;
        *changed.pointer_mut(pointer).ok_or("tamper field")? = serde_json::json!(hex(&raw));
        assert!(verified(changed, &authority).is_err(), "{pointer}");
        println!("GATEWAY_MAINTENANCE_CASE tamper-{pointer}");
    }
    let foreign = SequencerAuthorization::new(
        authority.sequencer_id(),
        [0; 32],
        authority.first_batch_number(),
        authority.last_batch_number(),
    );
    assert!(verified(head.clone(), &foreign).is_err());
    println!("GATEWAY_MAINTENANCE_CASE foreign-key");
    let excluded = SequencerAuthorization::new(
        authority.sequencer_id(),
        authority.public_key(),
        u64::MAX,
        u64::MAX,
    );
    assert!(verified(head, &excluded).is_err());
    println!("GATEWAY_MAINTENANCE_CASE unauthorized-batch");
    Ok(())
}

#[test]
fn stale_activity_and_changed_sequence_timestamp_are_refused() -> Result<(), String> {
    let (capture, authority) = capture()?;
    let head = envelope(&capture, true)?;
    for field in ["observed_sequence", "observed_at"] {
        let mut changed = head.clone();
        let value = changed[field].as_u64().ok_or("head integer missing")?;
        changed[field] = serde_json::json!(value.checked_add(1).ok_or("head integer overflow")?);
        assert!(verified(changed, &authority).is_err());
        println!("GATEWAY_MAINTENANCE_CASE stale-{field}");
    }
    let ordinary = envelope(&capture, false)?;
    assert_ne!(ordinary["state_root"], head["state_root"]);
    let mut stale = head.clone();
    stale["receipt_hex"] = ordinary["receipt_hex"].clone();
    stale["receipt_digest"] = ordinary["receipt_digest"].clone();
    assert!(verified(stale, &authority).is_err());
    println!("GATEWAY_MAINTENANCE_CASE stale-activity-receipt");
    let mut stale = head;
    stale["current"] = serde_json::json!(false);
    let parsed: HeadDocument = serde_json::from_value(stale).map_err(|error| error.to_string())?;
    assert!(!parsed.current);
    println!("GATEWAY_MAINTENANCE_CASE not-current-route-refusal");
    Ok(())
}
