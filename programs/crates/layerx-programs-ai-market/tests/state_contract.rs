use layerx_programs_ai_market::state::ReplayDecision;
use layerx_programs_ai_market::{codec, errors::*, state::*, types::*, MAX_STATE_BYTES};

fn value<T>(result: CodecResult<T>) -> T {
    result.unwrap_or_else(|error| panic!("{error}"))
}
fn hex(text: &str) -> Vec<u8> {
    text.as_bytes()
        .chunks_exact(2)
        .map(|pair| {
            let digit = |b| match b {
                b'0'..=b'9' => b - b'0',
                b'a'..=b'f' => b - b'a' + 10,
                _ => panic!("hex"),
            };
            digit(pair[0]) * 16 + digit(pair[1])
        })
        .collect()
}
fn empty() -> SharedState<'static> {
    SharedState {
        revision: 1,
        feature_sections: [&[]; 5],
        control: Control {
            replay: ReplayTable::new(),
            feature_bytes: &[],
        },
    }
}
fn encoded(state: &SharedState<'_>) -> Vec<u8> {
    let mut out = vec![0; value(state.encoded_len())];
    let mut scratch = vec![0; Section::Control.payload_cap()];
    let n = value(encode_shared_state(state, &mut out, &mut scratch));
    assert_eq!(n, out.len());
    out
}
fn request(slot: ActorSlot) -> ReplayRequest {
    ReplayRequest {
        slot,
        principal: value(PrincipalId::new([0x11; 32])),
        authority_version: value(Version::new(1)),
        sequence: 1,
        request_id: value(RequestId::new([0x22; 32])),
        digest: value(RequestDigest::new([0x33; 32])),
        expiry_height: 100,
    }
}
fn result() -> ResultDigest {
    value(ResultDigest::new([0x44; 32]))
}
fn bound() -> SharedState<'static> {
    let mut state = empty();
    let req = request(ActorSlot::OWNER);
    value(
        state
            .control
            .replay
            .bind(req.slot, req.principal, req.authority_version),
    );
    state
}

#[test]
fn fixed_six_section_bytes_and_exact_roundtrip() {
    let expected = hex(concat!(
        "50415841533100010000000000000001",
        "0001000000000000",
        "0002000000000000",
        "0003000000000000",
        "0004000000000000",
        "0005000000000000",
        "000600000000000a",
        "00010000000000000000"
    ));
    assert_eq!(expected.len(), 74);
    assert_eq!(encoded(&empty()), expected);
    assert_eq!(value(decode_shared_state(&expected)), empty());
    assert_eq!(encoded(&value(decode_shared_state(&expected))), expected);
    let frame = value(codec::decode_state(&expected));
    assert_eq!(frame.revision, 1);
    assert_eq!(frame.sections[5], hex("00010000000000000000"));
}

#[test]
fn state_refuses_magic_schema_revision_index_reserved_truncation_and_trailing() {
    let good = encoded(&empty());
    for end in 0..good.len() {
        assert!(decode_shared_state(&good[..end]).is_err(), "prefix {end}");
    }
    for offset in [0usize, 18, 26, 34, 42, 50, 58, 67] {
        let mut bad = good.clone();
        bad[offset] = 1;
        assert_eq!(
            decode_shared_state(&bad),
            Err(NON_CANONICAL),
            "offset {offset}"
        );
    }
    let mut bad = good.clone();
    bad[7] = 2;
    assert_eq!(decode_shared_state(&bad), Err(BAD_VERSION));
    let mut bad = good.clone();
    bad[65] = 2;
    assert_eq!(decode_shared_state(&bad), Err(BAD_VERSION));
    let mut bad = good.clone();
    bad[15] = 0;
    assert_eq!(decode_shared_state(&bad), Err(NON_CANONICAL));
    let mut bad = good.clone();
    bad[17] = 2;
    assert_eq!(decode_shared_state(&bad), Err(NON_CANONICAL));
    let mut bad = good.clone();
    bad[20..24].copy_from_slice(&u32::MAX.to_be_bytes());
    assert_eq!(decode_shared_state(&bad), Err(CAPACITY));
    let mut bad = good.clone();
    bad.push(0);
    assert_eq!(decode_shared_state(&bad), Err(NON_CANONICAL));
    assert_eq!(
        decode_shared_state(&vec![0; MAX_STATE_BYTES + 1]),
        Err(CAPACITY)
    );
}

#[test]
fn every_inclusive_section_cap_and_total_includes_all_framing() {
    let sections = [
        Section::PolicyLifecycle,
        Section::IdentityRoster,
        Section::CurrentReports,
        Section::SettlementClaims,
        Section::ReputationAdmission,
    ];
    for section in sections {
        let bytes = vec![0x5a; section.payload_cap()];
        let base = empty();
        let state = value(base.replace_section(section, &bytes));
        let encoded = encoded(&state);
        assert_eq!(
            value(codec::decode_state(&encoded)).sections[section.index()].len() + 8,
            codec::STATE_SECTION_CAPS[section.index()]
        );
        assert_eq!(value(decode_shared_state(&encoded)), state);
        let oversized = vec![0; section.payload_cap() + 1];
        assert_eq!(base.replace_section(section, &oversized), Err(CAPACITY));
        assert_eq!(base, empty());
    }
    let feature = vec![0x5a; Section::Control.payload_cap() - CONTROL_FIXED_BYTES];
    let mut state = empty();
    state.control.feature_bytes = &feature;
    let bytes = encoded(&state);
    assert_eq!(
        value(codec::decode_state(&bytes)).sections[5].len() + 8 + 16,
        codec::STATE_SECTION_CAPS[5]
    );
    let oversized = vec![0; feature.len() + 1];
    state.control.feature_bytes = &oversized;
    assert_eq!(state.encoded_len(), Err(CAPACITY));
    let payloads: [Vec<u8>; 5] =
        core::array::from_fn(|i| vec![0x5a; codec::STATE_SECTION_CAPS[i] - 8]);
    state.feature_sections = core::array::from_fn(|i| payloads[i].as_slice());
    state.control.feature_bytes = &feature;
    assert_eq!(value(state.encoded_len()), MAX_STATE_BYTES);
    let maximum = encoded(&state);
    assert_eq!(maximum.len(), 196_608);
    assert_eq!(value(decode_shared_state(&maximum)), state);
    assert_eq!(encoded(&value(decode_shared_state(&maximum))), maximum);
    let mut one_past = maximum.clone();
    one_past.push(0);
    assert_eq!(decode_shared_state(&one_past), Err(CAPACITY));
    state.control.feature_bytes = &oversized;
    let mut output = vec![0xa5; MAX_STATE_BYTES + 1];
    let mut scratch = vec![0xa5; Section::Control.payload_cap() + 1];
    assert_eq!(
        encode_shared_state(&state, &mut output, &mut scratch),
        Err(CAPACITY)
    );
    assert!(output.iter().all(|b| *b == 0xa5));
    assert!(scratch.iter().all(|b| *b == 0xa5));
}

#[test]
fn short_output_and_invalid_control_refuse_before_state_output() {
    let state = empty();
    let mut output = [0xa5; 73];
    let mut scratch = [0xa5; 10];
    assert_eq!(
        encode_shared_state(&state, &mut output, &mut scratch),
        Err(CAPACITY)
    );
    assert_eq!(output, [0xa5; 73]);
    assert_eq!(scratch, [0xa5; 10]);
    let mut output = [0xa5; 74];
    let mut scratch = [0xa5; 9];
    assert_eq!(
        encode_shared_state(&state, &mut output, &mut scratch),
        Err(CAPACITY)
    );
    assert_eq!(output, [0xa5; 74]);
    assert_eq!(scratch, [0xa5; 9]);
    let mut control = hex("00010000000000000000");
    control[3] = 1;
    assert_eq!(decode_control(&control), Err(NON_CANONICAL));
    let mut control = hex("00010000000000000000");
    control[9] = 1;
    assert!(decode_control(&control).is_err());
    assert!(empty().section(Section::Control).is_err());
}

#[test]
fn fixed_actor_replay_bytes_and_result_retention() {
    let mut state = bound();
    let req = request(ActorSlot::OWNER);
    let expected_empty = hex(concat!(
        "00010000",
        "1111111111111111111111111111111111111111111111111111111111111111",
        "000000000000000100"
    ));
    let mut bytes = vec![0; value(state.control.replay.encoded_len())];
    assert_eq!(value(encode_replay(&state.control.replay, &mut bytes)), 45);
    assert_eq!(bytes, expected_empty);
    assert_eq!(value(decode_replay(&bytes)), state.control.replay);
    assert_eq!(
        value(state.record_success(&req, 99, result())),
        ReplayDecision::Apply
    );
    assert_eq!(state.revision, 2);
    let expected = hex(concat!(
        "00010000",
        "1111111111111111111111111111111111111111111111111111111111111111",
        "0000000000000001010000000000000001",
        "2222222222222222222222222222222222222222222222222222222222222222",
        "3333333333333333333333333333333333333333333333333333333333333333",
        "4444444444444444444444444444444444444444444444444444444444444444",
        "00000000000000020000000000000064"
    ));
    let mut bytes = vec![0; value(state.control.replay.encoded_len())];
    assert_eq!(value(encode_replay(&state.control.replay, &mut bytes)), 165);
    assert_eq!(bytes, expected);
    let table = value(decode_replay(&expected));
    assert_eq!(table, state.control.replay);
    let before = state.clone();
    let retry = value(state.record_success(&req, 99, value(ResultDigest::new([0x55; 32]))));
    match retry {
        ReplayDecision::AlreadyApplied(retained) => {
            assert_eq!(retained.result_digest, result());
            assert_eq!(retained.applied_revision, 2);
            assert_eq!(retained.sequence, 1);
            assert_eq!(retained.request_id, req.request_id);
        }
        ReplayDecision::Apply => panic!("retry reapplied"),
    }
    assert_eq!(state, before);
    assert_eq!(value(decode_shared_state(&encoded(&state))), state);
    for end in 0..expected.len() {
        assert!(decode_replay(&expected[..end]).is_err());
    }
    let mut trailing = expected.clone();
    trailing.push(0);
    assert_eq!(decode_replay(&trailing), Err(NON_CANONICAL));
}

#[test]
fn replay_refusals_are_atomic_and_only_last_success_is_retained() {
    let mut state = bound();
    let req = request(ActorSlot::OWNER);
    let initial = state.clone();
    for bad in [
        ReplayRequest { sequence: 0, ..req },
        ReplayRequest { sequence: 2, ..req },
        ReplayRequest {
            expiry_height: 0,
            ..req
        },
        ReplayRequest {
            principal: value(PrincipalId::new([9; 32])),
            ..req
        },
        ReplayRequest {
            authority_version: value(Version::new(2)),
            ..req
        },
    ] {
        assert!(state.record_success(&bad, 99, result()).is_err());
        assert_eq!(state, initial);
    }
    assert_eq!(state.record_success(&req, 100, result()), Err(EXPIRED));
    assert_eq!(state, initial);
    value(state.record_success(&req, 99, result()));
    let first = state.clone();
    assert_eq!(
        state.record_success(
            &ReplayRequest {
                digest: value(RequestDigest::new([8; 32])),
                ..req
            },
            99,
            result()
        ),
        Err(REPLAY_CONFLICT)
    );
    assert_eq!(state, first);
    assert_eq!(
        state.record_success(
            &ReplayRequest {
                request_id: value(RequestId::new([8; 32])),
                ..req
            },
            99,
            result()
        ),
        Err(REPLAY_CONFLICT)
    );
    assert_eq!(state, first);
    assert_eq!(
        state.record_success(&ReplayRequest { sequence: 3, ..req }, 99, result()),
        Err(SEQUENCE_GAP)
    );
    assert_eq!(state, first);
    assert_eq!(state.record_success(&req, 100, result()), Err(EXPIRED));
    assert_eq!(state, first);
    let next = ReplayRequest {
        sequence: 2,
        request_id: value(RequestId::new([5; 32])),
        digest: value(RequestDigest::new([6; 32])),
        ..req
    };
    value(state.record_success(&next, 99, result()));
    let second = state.clone();
    assert_eq!(
        state.record_success(&req, 99, result()),
        Err(SEQUENCE_CONSUMED)
    );
    assert_eq!(state, second);
    assert_eq!(
        state
            .control
            .replay
            .actor(req.slot)
            .and_then(|a| a.last)
            .map(|r| r.sequence),
        Some(2)
    );
}

#[test]
fn replay_slot_bounds_eviction_and_operator_grant_versions() {
    assert!(ActorSlot::worker(32).is_err());
    assert!(ActorSlot::evaluator(8).is_err());
    assert!(ActorSlot::from_index(43).is_err());
    assert_eq!(value(ActorSlot::worker(31)).index(), 34);
    assert_eq!(value(ActorSlot::evaluator(7)).index(), 42);
    let mut table = ReplayTable::new();
    let req = request(value(ActorSlot::worker(0)));
    value(table.bind(req.slot, req.principal, req.authority_version));
    let before = table.clone();
    assert_eq!(
        table.bind(req.slot, req.principal, req.authority_version),
        Err(CONFLICT)
    );
    assert_eq!(table, before);
    let mut revision = 1;
    value(table.record_success(&req, 99, &mut revision, result()));
    value(table.retire(req.slot));
    assert_eq!(table.check(&req, 99), Err(NOT_FOUND));
    assert_eq!(table.retire(req.slot), Err(NOT_FOUND));
    for slot in [ActorSlot::OWNER, ActorSlot::TREASURY, ActorSlot::OPERATOR] {
        assert_eq!(table.retire(slot), Err(UNAUTHORIZED));
    }
    let op = request(ActorSlot::OPERATOR);
    value(table.bind(op.slot, op.principal, op.authority_version));
    value(table.record_success(&op, 99, &mut revision, result()));
    let before = table.clone();
    assert_eq!(
        table.replace_operator(op.principal, op.authority_version),
        Err(CONFLICT)
    );
    assert_eq!(table, before);
    value(table.replace_operator(op.principal, value(Version::new(2))));
    assert_eq!(table.check(&op, 99), Err(UNAUTHORIZED));
    assert_eq!(
        value(table.check(
            &ReplayRequest {
                authority_version: value(Version::new(2)),
                ..op
            },
            99
        )),
        ReplayDecision::Apply
    );
}

#[test]
fn maximum_actor_table_is_bounded_canonical_and_count_index_flags_are_strict() {
    let mut state = empty();
    for i in 0..43 {
        let req = request(value(ActorSlot::from_index(i)));
        value(
            state
                .control
                .replay
                .bind(req.slot, req.principal, req.authority_version),
        );
        value(state.record_success(&req, 99, result()));
    }
    assert_eq!(state.revision, 44);
    assert_eq!(value(state.control.replay.encoded_len()), 2 + 43 * 163);
    let bytes = encoded(&state);
    assert_eq!(value(decode_shared_state(&bytes)), state);
    let mut table = vec![0; value(state.control.replay.encoded_len())];
    value(encode_replay(&state.control.replay, &mut table));
    assert_eq!(value(decode_replay(&table)), state.control.replay);
    assert_eq!(decode_replay(&[0, 44]), Err(CAPACITY));
    for (offset, byte) in [(3, 43), (44, 2), (166, 0)] {
        let mut bad = table.clone();
        bad[offset] = byte;
        assert_eq!(decode_replay(&bad), Err(NON_CANONICAL), "offset {offset}");
    }
    let mut bad = table.clone();
    bad[4..36].fill(0);
    assert_eq!(decode_replay(&bad), Err(NON_CANONICAL));
    let mut bad = table.clone();
    bad[36..44].fill(0);
    assert_eq!(decode_replay(&bad), Err(NON_CANONICAL));
    for range in [45..53, 149..157, 157..165] {
        let mut bad = table.clone();
        bad[range].fill(0);
        assert_eq!(decode_replay(&bad), Err(NON_CANONICAL));
    }
    let mut bad = bytes.clone();
    bad[15] = 1;
    assert_eq!(decode_shared_state(&bad), Err(NON_CANONICAL));
}

#[test]
fn replay_growth_and_revision_overflow_refuse_without_partial_transition() {
    let mut state = bound();
    let req = request(ActorSlot::OWNER);
    state.revision = u64::MAX;
    let before = state.clone();
    assert_eq!(state.record_success(&req, 99, result()), Err(ARITHMETIC));
    assert_eq!(state, before);
    let mut state = bound();
    let suffix = vec![0; Section::Control.payload_cap() - value(state.control.encoded_len())];
    state.control.feature_bytes = &suffix;
    let before = state.clone();
    assert_eq!(
        value(state.control.encoded_len()),
        Section::Control.payload_cap()
    );
    assert_eq!(state.record_success(&req, 99, result()), Err(CAPACITY));
    assert_eq!(state, before);
    let mut table = bound().control.replay;
    let before = table.clone();
    let mut revision = 0;
    assert_eq!(
        table.record_success(&req, 99, &mut revision, result()),
        Err(NON_CANONICAL)
    );
    assert_eq!(table, before);
    assert_eq!(revision, 0);
}

#[test]
fn epoch_windows_are_half_open_and_checked() {
    let work = value(HeightWindow::epoch(0, 0, 0, 64));
    let commit = value(HeightWindow::epoch(0, 0, 64, 80));
    let reveal = value(HeightWindow::epoch(0, 0, 80, 96));
    let settle = value(HeightWindow::epoch(0, 0, 96, 128));
    value(work.check(63));
    assert_eq!(work.check(64), Err(WRONG_PHASE));
    value(commit.check(64));
    value(commit.check(79));
    assert_eq!(commit.check(80), Err(WRONG_PHASE));
    value(reveal.check(80));
    value(reveal.check(95));
    assert_eq!(reveal.check(96), Err(WRONG_PHASE));
    value(settle.check(96));
    assert_eq!(settle.check(128), Err(WRONG_PHASE));
    let later = value(HeightWindow::epoch(7, 2, 0, 64));
    assert_eq!(later.start, 263);
    assert_eq!(later.end, 327);
    assert_eq!(later.check(262), Err(WRONG_PHASE));
    assert_eq!(HeightWindow::epoch(u64::MAX, 1, 0, 64), Err(ARITHMETIC));
    assert_eq!(HeightWindow::epoch(0, u64::MAX, 0, 64), Err(ARITHMETIC));
    assert_eq!(
        HeightWindow::epoch(u64::MAX - 63, 0, 0, 64),
        Err(ARITHMETIC)
    );
    assert_eq!(HeightWindow::epoch(0, 0, 64, 64), Err(NON_CANONICAL));
    assert_eq!(HeightWindow::epoch(0, 0, 0, 129), Err(NON_CANONICAL));
    assert_eq!(
        HeightWindow { start: 1, end: 0 }.check(0),
        Err(NON_CANONICAL)
    );
}

#[test]
fn maximum_sequence_never_wraps_and_digest_absence_is_refused() {
    let req = request(ActorSlot::OWNER);
    let mut state = bound();
    value(state.record_success(&req, 99, result()));
    let mut bytes = vec![0; value(state.control.replay.encoded_len())];
    value(encode_replay(&state.control.replay, &mut bytes));
    for range in [53..85, 85..117, 117..149] {
        let mut bad = bytes.clone();
        bad[range].fill(0);
        assert_eq!(decode_replay(&bad), Err(NON_CANONICAL));
    }
    bytes[45..53].copy_from_slice(&u64::MAX.to_be_bytes());
    let mut table = value(decode_replay(&bytes));
    let before = table.clone();
    let mut revision = 2;
    let max = ReplayRequest {
        sequence: u64::MAX,
        ..req
    };
    assert!(matches!(
        value(table.record_success(&max, 99, &mut revision, result())),
        ReplayDecision::AlreadyApplied(_)
    ));
    assert_eq!(table, before);
    assert_eq!(revision, 2);
    assert_eq!(
        table.record_success(
            &ReplayRequest { sequence: 0, ..req },
            99,
            &mut revision,
            result()
        ),
        Err(NON_CANONICAL)
    );
    assert_eq!(
        table.record_success(&req, 99, &mut revision, result()),
        Err(SEQUENCE_CONSUMED)
    );
    assert_eq!(table, before);
    assert_eq!(revision, 2);
    let mut output = vec![0xa5; bytes.len() - 1];
    assert_eq!(encode_replay(&table, &mut output), Err(CAPACITY));
    assert!(output.iter().all(|b| *b == 0xa5));
}
