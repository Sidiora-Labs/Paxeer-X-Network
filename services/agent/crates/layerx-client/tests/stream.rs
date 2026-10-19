use std::fs;
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::PathBuf;
use std::sync::atomic::{AtomicU64, Ordering};
use std::thread;
use std::time::Duration;

use layerx_client::head::Head;
use layerx_client::lni::framing::{read_frame, write_frame};
use layerx_client::lni::schema::{decode_envelope, encode_envelope, Envelope, Version};
use layerx_client::lni::transport::{ConnectionGate, Limits, Uds};
use layerx_client::stream::{subscribe, Cursor, StreamConfig, StreamError, StreamItem};

static NEXT_SOCKET: AtomicU64 = AtomicU64::new(1);

struct SocketPath(PathBuf);

impl SocketPath {
    fn new(label: &str) -> Self {
        let sequence = NEXT_SOCKET.fetch_add(1, Ordering::Relaxed);
        Self(std::env::temp_dir().join(format!(
            "layerx-stream-{label}-{}-{sequence}.sock",
            std::process::id()
        )))
    }
}

impl Drop for SocketPath {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.0);
    }
}

fn limits() -> Limits {
    Limits {
        maximum_frame_bytes: 1024 * 1024,
        maximum_connections: 1,
        maximum_streams: 4,
        maximum_queued_bytes: 2 * 1024 * 1024,
        deadline: Duration::from_secs(2),
    }
}

fn head() -> Head {
    Head {
        chain_sequence: 100,
        sealed_batch: 7,
        finalised_checkpoint: [0x71; 32],
    }
}

fn config(maximum_bytes: u32) -> StreamConfig {
    StreamConfig {
        interface_version: Version::V1_0,
        correlation_id: 50,
        maximum_buffered_events: 4,
        maximum_buffered_bytes: maximum_bytes,
        maximum_heartbeats_per_poll: 3,
    }
}

fn subscription(stream: &mut UnixStream) -> Cursor {
    let frame = match read_frame(stream, 1024 * 1024) {
        Ok(frame) => frame,
        Err(error) => panic!("subscription frame failed: {error:?}"),
    };
    let request = match decode_envelope(&frame) {
        Ok(request) => request,
        Err(error) => panic!("subscription envelope failed: {error:?}"),
    };
    assert_eq!(request.message_tag, 21);
    let Some(token) = request.canonical_payload.get(..48) else {
        panic!("subscription cursor truncated");
    };
    match Cursor::decode(token) {
        Ok(cursor) => cursor,
        Err(error) => panic!("subscription cursor failed: {error:?}"),
    }
}

fn event(stream: &mut UnixStream, sequence: u64, bytes: &[u8]) {
    let mut payload = sequence.to_be_bytes().to_vec();
    payload.extend_from_slice(bytes);
    response(stream, 22, &payload);
}

fn response(stream: &mut UnixStream, tag: u16, payload: &[u8]) {
    let encoded = match encode_envelope(Envelope {
        version: Version::V1_0,
        message_tag: tag,
        correlation_id: 50,
        canonical_payload: payload,
        proof_material: &[],
    }) {
        Ok(encoded) => encoded,
        Err(error) => panic!("event response encoding failed: {error:?}"),
    };
    if let Err(error) = write_frame(stream, &encoded, 1024 * 1024) {
        panic!("event response failed: {error:?}");
    }
}

#[test]
fn disconnect_cursor_survives_restart_without_gap_or_duplicate() {
    let socket = SocketPath::new("restart");
    let listener = match UnixListener::bind(&socket.0) {
        Ok(listener) => listener,
        Err(error) => panic!("listener failed: {error}"),
    };
    let server = thread::spawn(move || {
        let (mut first, _) = match listener.accept() {
            Ok(connection) => connection,
            Err(error) => panic!("first accept failed: {error}"),
        };
        assert_eq!(subscription(&mut first).next_sequence(), 10);
        event(&mut first, 10, b"event-ten");
        drop(first);
        let (mut second, _) = match listener.accept() {
            Ok(connection) => connection,
            Err(error) => panic!("second accept failed: {error}"),
        };
        assert_eq!(subscription(&mut second).next_sequence(), 11);
        event(&mut second, 11, b"event-eleven");
    });
    let gate = ConnectionGate::new(1);
    let cursor = {
        let mut transport = match Uds::connect(&socket.0, &gate, limits()) {
            Ok(transport) => transport,
            Err(error) => panic!("first connection failed: {error:?}"),
        };
        let mut stream = match subscribe(&mut transport, Cursor::new(10, head()), config(1024)) {
            Ok(stream) => stream,
            Err(error) => panic!("first subscribe failed: {error:?}"),
        };
        let first = match stream.next_item() {
            Ok(StreamItem::Event(event)) => event,
            Ok(StreamItem::Gap(gap)) => panic!("unexpected first gap: {gap:?}"),
            Err(error) => panic!("first event failed: {error:?}"),
        };
        assert_eq!(first.global_sequence, 10);
        assert_eq!(first.canonical_bytes(), b"event-ten");
        let disconnected = stream.next_item();
        assert!(matches!(
            disconnected,
            Err(StreamError::Disconnected { cursor, .. }) if cursor.next_sequence() == 11
        ));
        let encoded = first.cursor().encode();
        match Cursor::decode(&encoded) {
            Ok(cursor) => cursor,
            Err(error) => panic!("persisted cursor failed: {error:?}"),
        }
    };
    {
        let mut transport = match Uds::connect(&socket.0, &gate, limits()) {
            Ok(transport) => transport,
            Err(error) => panic!("restart connection failed: {error:?}"),
        };
        let mut stream = match subscribe(&mut transport, cursor, config(1024)) {
            Ok(stream) => stream,
            Err(error) => panic!("restart subscribe failed: {error:?}"),
        };
        let second = match stream.next_item() {
            Ok(StreamItem::Event(event)) => event,
            Ok(StreamItem::Gap(gap)) => panic!("unexpected restart gap: {gap:?}"),
            Err(error) => panic!("restart event failed: {error:?}"),
        };
        assert_eq!(second.global_sequence, 11);
        assert_eq!(second.canonical_bytes(), b"event-eleven");
    }
    assert!(server.join().is_ok(), "restart node panicked");
}

#[test]
fn out_of_order_node_is_refused_without_reordering() {
    let socket = SocketPath::new("order");
    let listener = match UnixListener::bind(&socket.0) {
        Ok(listener) => listener,
        Err(error) => panic!("listener failed: {error}"),
    };
    let server = thread::spawn(move || {
        let (mut stream, _) = match listener.accept() {
            Ok(connection) => connection,
            Err(error) => panic!("accept failed: {error}"),
        };
        let _ = subscription(&mut stream);
        event(&mut stream, 9, b"old-event");
    });
    let gate = ConnectionGate::new(1);
    let mut transport = match Uds::connect(&socket.0, &gate, limits()) {
        Ok(transport) => transport,
        Err(error) => panic!("connection failed: {error:?}"),
    };
    let mut stream = match subscribe(&mut transport, Cursor::new(10, head()), config(1024)) {
        Ok(stream) => stream,
        Err(error) => panic!("subscribe failed: {error:?}"),
    };
    assert_eq!(
        stream.next_item(),
        Err(StreamError::OutOfOrder {
            expected: 10,
            actual: 9,
        })
    );
    assert!(server.join().is_ok(), "order node panicked");
}

#[test]
fn skipped_sequence_surfaces_gap_and_backfill_cursor() {
    let socket = SocketPath::new("gap");
    let listener = match UnixListener::bind(&socket.0) {
        Ok(listener) => listener,
        Err(error) => panic!("listener failed: {error}"),
    };
    let server = thread::spawn(move || {
        let (mut first, _) = match listener.accept() {
            Ok(connection) => connection,
            Err(error) => panic!("first accept failed: {error}"),
        };
        let _ = subscription(&mut first);
        event(&mut first, 12, b"past-gap");
        drop(first);
        let (mut backfill, _) = match listener.accept() {
            Ok(connection) => connection,
            Err(error) => panic!("backfill accept failed: {error}"),
        };
        assert_eq!(subscription(&mut backfill).next_sequence(), 10);
        event(&mut backfill, 10, b"backfilled-ten");
    });
    let gate = ConnectionGate::new(1);
    let backfill_cursor = {
        let mut transport = match Uds::connect(&socket.0, &gate, limits()) {
            Ok(transport) => transport,
            Err(error) => panic!("first connection failed: {error:?}"),
        };
        let mut stream = match subscribe(&mut transport, Cursor::new(10, head()), config(1024)) {
            Ok(stream) => stream,
            Err(error) => panic!("first subscribe failed: {error:?}"),
        };
        let gap = match stream.next_item() {
            Ok(StreamItem::Gap(gap)) => gap,
            Ok(StreamItem::Event(event)) => panic!("gap delivered event: {event:?}"),
            Err(error) => panic!("gap detection failed: {error:?}"),
        };
        assert_eq!(gap.missing_first, 10);
        assert_eq!(gap.missing_last, 11);
        gap.backfill_cursor()
    };
    {
        let mut transport = match Uds::connect(&socket.0, &gate, limits()) {
            Ok(transport) => transport,
            Err(error) => panic!("backfill connection failed: {error:?}"),
        };
        let mut stream = match subscribe(&mut transport, backfill_cursor, config(1024)) {
            Ok(stream) => stream,
            Err(error) => panic!("backfill subscribe failed: {error:?}"),
        };
        let event = match stream.next_item() {
            Ok(StreamItem::Event(event)) => event,
            Ok(StreamItem::Gap(gap)) => panic!("backfill returned gap: {gap:?}"),
            Err(error) => panic!("backfill event failed: {error:?}"),
        };
        assert_eq!(event.global_sequence, 10);
        assert_eq!(event.canonical_bytes(), b"backfilled-ten");
    }
    assert!(server.join().is_ok(), "gap node panicked");
}

#[test]
fn oversized_record_applies_explicit_backpressure_at_cursor() {
    let socket = SocketPath::new("backpressure");
    let listener = match UnixListener::bind(&socket.0) {
        Ok(listener) => listener,
        Err(error) => panic!("listener failed: {error}"),
    };
    let server = thread::spawn(move || {
        let (mut stream, _) = match listener.accept() {
            Ok(connection) => connection,
            Err(error) => panic!("accept failed: {error}"),
        };
        let _ = subscription(&mut stream);
        event(&mut stream, 10, &[0xaa; 64]);
    });
    let gate = ConnectionGate::new(1);
    let mut transport = match Uds::connect(&socket.0, &gate, limits()) {
        Ok(transport) => transport,
        Err(error) => panic!("connection failed: {error:?}"),
    };
    let mut stream = match subscribe(&mut transport, Cursor::new(10, head()), config(32)) {
        Ok(stream) => stream,
        Err(error) => panic!("subscribe failed: {error:?}"),
    };
    assert!(matches!(
        stream.next_item(),
        Err(StreamError::Backpressure { cursor, .. }) if cursor.next_sequence() == 10
    ));
    assert!(server.join().is_ok(), "backpressure node panicked");
}
