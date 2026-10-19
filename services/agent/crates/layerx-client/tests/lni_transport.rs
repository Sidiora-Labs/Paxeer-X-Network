use std::io::Write as _;
use std::os::unix::net::UnixStream;
use std::time::Duration;

use layerx_client::lni::framing::{decode_frame, read_frame, write_frame, DecodedFrame};
use layerx_client::lni::transport::{
    ConnectionGate, FrameViolation, Limits, Multiplexer, OutboundFrame, TrafficClass,
    TransportError,
};

fn limits() -> Limits {
    Limits {
        maximum_frame_bytes: 64,
        maximum_connections: 1,
        maximum_streams: 2,
        maximum_queued_bytes: 12,
        deadline: Duration::from_millis(20),
    }
}

#[test]
fn rejects_truncated_and_oversized_frames_before_body_allocation() {
    assert_eq!(decode_frame(&[0, 0, 0], 64), Ok(DecodedFrame::Incomplete));
    assert_eq!(
        decode_frame(&[0, 0, 0, 3, 1, 2], 64),
        Ok(DecodedFrame::Incomplete)
    );
    assert_eq!(
        decode_frame(&u32::MAX.to_be_bytes(), 64),
        Err(FrameViolation::Oversized {
            declared: u32::MAX,
            maximum: 64,
        })
    );

    let mut truncated_body = &b"\0\0\0\x03ab"[..];
    assert_eq!(
        read_frame(&mut truncated_body, 64),
        Err(TransportError::Frame(FrameViolation::TruncatedBody))
    );
}

#[test]
fn interleaved_streams_prioritise_submission_and_surface_backpressure() {
    let mut mux = Multiplexer::new(limits())
        .unwrap_or_else(|error| panic!("valid limits rejected: {error:?}"));
    assert_eq!(
        mux.enqueue(OutboundFrame {
            stream_id: 10,
            class: TrafficClass::BulkStream,
            bytes: vec![1; 6],
        }),
        Ok(())
    );
    assert_eq!(
        mux.enqueue(OutboundFrame {
            stream_id: 20,
            class: TrafficClass::Submission,
            bytes: vec![2; 4],
        }),
        Ok(())
    );
    assert_eq!(
        mux.enqueue(OutboundFrame {
            stream_id: 20,
            class: TrafficClass::Submission,
            bytes: vec![3; 3],
        }),
        Err(TransportError::Backpressure)
    );
    let first = mux
        .pop_next()
        .unwrap_or_else(|| panic!("submission was not scheduled"));
    assert_eq!(first.stream_id, 20);
    assert_eq!(first.class, TrafficClass::Submission);
    assert_eq!(mux.queued_bytes(), 6);
}

#[test]
fn peer_shutdown_deadline_and_connection_limit_remain_distinct() {
    let gate = ConnectionGate::new(1);
    let permit = gate
        .acquire()
        .unwrap_or_else(|error| panic!("first connection denied: {error:?}"));
    assert_eq!(
        gate.acquire().map(|_| ()),
        Err(TransportError::ConnectionLimit)
    );
    drop(permit);
    assert_eq!(gate.active(), 0);

    let (mut reader, writer) =
        UnixStream::pair().unwrap_or_else(|error| panic!("Unix pair creation failed: {error}"));
    reader
        .set_read_timeout(Some(Duration::from_millis(20)))
        .unwrap_or_else(|error| panic!("read deadline failed: {error}"));
    assert_eq!(read_frame(&mut reader, 64), Err(TransportError::Deadline));
    drop(writer);
    assert_eq!(
        read_frame(&mut reader, 64),
        Err(TransportError::PeerShutdown)
    );
}

#[test]
fn abrupt_loss_and_round_trip_preserve_exact_payloads() {
    let (mut left, mut right) =
        UnixStream::pair().unwrap_or_else(|error| panic!("Unix pair creation failed: {error}"));
    write_frame(&mut left, b"canonical", 64)
        .unwrap_or_else(|error| panic!("frame write failed: {error:?}"));
    assert_eq!(read_frame(&mut right, 64), Ok(b"canonical".to_vec()));

    right
        .write_all(&[0, 0])
        .unwrap_or_else(|error| panic!("partial write failed: {error}"));
    drop(right);
    assert_eq!(
        read_frame(&mut left, 64),
        Err(TransportError::Frame(FrameViolation::TruncatedPrefix))
    );
}
