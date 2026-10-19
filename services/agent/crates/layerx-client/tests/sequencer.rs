use std::fmt::Write as _;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::TcpListener;
use std::thread::JoinHandle;

use layerx_client::sequencer::{
    CallRequest, ExecutedReceipt, ReceiptKeyEntry, RpcRefusal, SequencerBlock, SequencerEndpoint,
    SequencerError, SequencerLog, SequencerReceipt,
};
use layerx_crypto::evm_transaction::Eip1559Call;
use serde_json::{json, Value};

trait Must<T> {
    fn must(self) -> T;
}

impl<T, E: std::fmt::Debug> Must<T> for Result<T, E> {
    fn must(self) -> T {
        self.unwrap_or_else(|error| panic!("{error:?}"))
    }
}

fn hex(bytes: &[u8]) -> String {
    bytes.iter().fold(String::new(), |mut text, byte| {
        let _ = write!(text, "{byte:02x}");
        text
    })
}

const TX_HASH: &str = "0x8f1c4b2a9e7d3c5b1a0f2e4d6c8b0a1928374655647382910a1b2c3d4e5f6071";

fn tx_hash() -> [u8; 32] {
    decode32(TX_HASH)
}

fn decode32(text: &str) -> [u8; 32] {
    let digits = text.trim_start_matches("0x");
    let mut out = [0_u8; 32];
    for (index, byte) in out.iter_mut().enumerate() {
        *byte = u8::from_str_radix(&digits[index * 2..index * 2 + 2], 16).must();
    }
    out
}

/// One recorded exchange: the method the client must call and the response
/// members after `jsonrpc` and `id`, or `None` to close without answering.
type Exchange = (&'static str, Option<String>);

/// Serves the exchanges in order on loopback, one connection each, and
/// returns the params of every request it received.
fn serve(exchanges: Vec<Exchange>) -> (String, JoinHandle<Vec<Value>>) {
    let listener = TcpListener::bind("127.0.0.1:0").must();
    let url = format!("http://127.0.0.1:{}/", listener.local_addr().must().port());
    let handle = std::thread::spawn(move || {
        let mut seen = Vec::new();
        for (method, members) in exchanges {
            let (stream, _) = listener.accept().must();
            let mut reader = BufReader::new(stream);
            let mut length = 0_usize;
            loop {
                let mut line = String::new();
                reader.read_line(&mut line).must();
                if line == "\r\n" {
                    break;
                }
                let lower = line.to_ascii_lowercase();
                if let Some(value) = lower.strip_prefix("content-length:") {
                    length = value.trim().parse().must();
                }
            }
            let mut body = vec![0_u8; length];
            reader.read_exact(&mut body).must();
            let request: Value = serde_json::from_slice(&body).must();
            assert_eq!(request["jsonrpc"], "2.0");
            assert_eq!(request["method"], method);
            seen.push(request["params"].clone());
            let Some(members) = members else {
                continue;
            };
            let answer = format!("{{\"jsonrpc\":\"2.0\",\"id\":{},{members}}}", request["id"]);
            let mut stream = reader.into_inner();
            write!(
                stream,
                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n{answer}",
                answer.len()
            )
            .must();
            stream.flush().must();
        }
        seen
    });
    (url, handle)
}

fn endpoint(url: &str) -> SequencerEndpoint {
    SequencerEndpoint::new(url, &[]).must()
}

fn receipted_members() -> String {
    format!(
        r#""rung":"receipted","txHash":"{TX_HASH}","nonce":"0x7","seq":"0x2a","height":"0x3e8","index":"0x3","timestamp":"0x67a1b2c3","status":"0x1","returnData":"0x0000000000000000000000000000000000000000000000000000000000000001","logs":[{{"address":"0x000000000000000000000000000000000000101a","topics":["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef","0x00000000000000000000000011223344556677889900aabbccddeeff00112233"],"data":"0x0de0b6b3a7640000","logIndex":"0x0"}},{{"address":"0x000000000000000000000000000000000000101A","topics":[],"data":"0x","logIndex":"0x1"}}],"gasUsed":"0x5208","cumulativeGasUsed":"0xa410","effectiveGasPrice":"0x3b9aca00","feeDenom":"upax","feeAmount":"0x1312d00000000","sponsor":"0x0000000000000000000000000000000000000000","from":"0x11223344556677889900aabbccddeeff00112233","to":"0x000000000000000000000000000000000000101a","contractAddress":"0x0000000000000000000000000000000000000000","vmError":"","keyId":"0x0101010101010101010101010101010101010101010101010101010101010101","receipt":"0x00025201000200aabbccdd""#
    )
}

fn expected_receipt() -> ExecutedReceipt {
    let mut amount = [0_u8; 32];
    amount[25..].copy_from_slice(&[0x01, 0x31, 0x2d, 0x00, 0x00, 0x00, 0x00]);
    let mut token = [0_u8; 20];
    token[18] = 0x10;
    token[19] = 0x1a;
    let mut sender_topic = [0_u8; 32];
    sender_topic[12..].copy_from_slice(&from());
    ExecutedReceipt {
        tx_hash: tx_hash(),
        nonce: 7,
        seq: 42,
        height: 1000,
        index: 3,
        timestamp: 0x67a1_b2c3,
        success: true,
        return_data: {
            let mut word = vec![0_u8; 32];
            word[31] = 1;
            word
        },
        logs: vec![
            SequencerLog {
                address: token,
                topics: vec![
                    decode32("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
                    sender_topic,
                ],
                data: vec![0x0d, 0xe0, 0xb6, 0xb3, 0xa7, 0x64, 0x00, 0x00],
                log_index: 0,
            },
            SequencerLog {
                address: token,
                topics: vec![],
                data: vec![],
                log_index: 1,
            },
        ],
        gas_used: 21_000,
        cumulative_gas_used: 42_000,
        effective_gas_price: 1_000_000_000,
        fee_denom: "upax".to_owned(),
        fee_amount: amount,
        sponsor: [0; 20],
        from: from(),
        to: token,
        contract_address: [0; 20],
        vm_error: String::new(),
        key_id: [1; 32],
        receipt: vec![
            0x00, 0x02, 0x52, 0x01, 0x00, 0x02, 0x00, 0xaa, 0xbb, 0xcc, 0xdd,
        ],
    }
}

fn from() -> [u8; 20] {
    [
        0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0xaa, 0xbb, 0xcc, 0xdd, 0xee,
        0xff, 0x00, 0x11, 0x22, 0x33,
    ]
}

#[test]
fn receipted_and_committed_receipts_decode_every_field() {
    let committed = format!(
        r#"{},"blockNumber":"0x3e9","blockHash":"0x{}""#,
        receipted_members().replace("\"receipted\"", "\"committed\""),
        "ab".repeat(32)
    );
    let (url, server) = serve(vec![
        (
            "pax_sequencerReceipt",
            Some(format!(r#""result":{{{}}}"#, receipted_members())),
        ),
        (
            "pax_sequencerReceipt",
            Some(format!(r#""result":{{{committed}}}"#)),
        ),
    ]);
    let endpoint = endpoint(&url);
    assert_eq!(
        endpoint.sequencer_receipt(&tx_hash()),
        Ok(Some(SequencerReceipt::Receipted(expected_receipt())))
    );
    assert_eq!(
        endpoint.sequencer_receipt(&tx_hash()),
        Ok(Some(SequencerReceipt::Committed {
            receipt: expected_receipt(),
            block_number: 1001,
            block_hash: [0xab; 32],
        }))
    );
    assert_eq!(
        server.join().must(),
        vec![json!([TX_HASH]), json!([TX_HASH])]
    );
}

#[test]
fn submitted_and_null_rungs_decode_and_other_shapes_are_refused() {
    let submitted =
        format!(r#""result":{{"rung":"submitted","txHash":"{TX_HASH}","nonce":"0x9"}}"#);
    let (url, server) = serve(vec![
        ("pax_sequencerReceipt", Some(submitted)),
        ("pax_sequencerReceipt", Some(r#""result":null"#.to_owned())),
        (
            "pax_sequencerReceipt",
            Some(format!(
                r#""result":{{"rung":"submitted","txHash":"{TX_HASH}","nonce":"0x9","seq":"0x1"}}"#
            )),
        ),
        (
            "pax_sequencerReceipt",
            Some(format!(
                r#""result":{{"rung":"submitted","txHash":"0x{}","nonce":"0x9"}}"#,
                "00".repeat(32)
            )),
        ),
        (
            "pax_sequencerReceipt",
            Some(format!(
                r#""result":{{{}}}"#,
                receipted_members().replace("\"0x7\"", "\"0x07\"")
            )),
        ),
        (
            "pax_sequencerReceipt",
            Some(format!(
                r#""result":{{{}}}"#,
                receipted_members().replace("\"receipted\"", "\"committed\"")
            )),
        ),
        (
            "pax_sequencerReceipt",
            Some(format!(
                r#""result":{{{},"blockNumber":"0x3e9","blockHash":"0x{}"}}"#,
                receipted_members(),
                "ab".repeat(32)
            )),
        ),
        (
            "pax_sequencerReceipt",
            Some(format!(
                r#""result":{{{}}}"#,
                receipted_members().replace("\"status\":\"0x1\"", "\"status\":\"0x2\"")
            )),
        ),
    ]);
    let endpoint = endpoint(&url);
    assert_eq!(
        endpoint.sequencer_receipt(&tx_hash()),
        Ok(Some(SequencerReceipt::Submitted {
            tx_hash: tx_hash(),
            nonce: 9
        }))
    );
    assert_eq!(endpoint.sequencer_receipt(&tx_hash()), Ok(None));
    for _ in 0..6 {
        assert_eq!(
            endpoint.sequencer_receipt(&tx_hash()),
            Err(SequencerError::Malformed)
        );
    }
    assert_eq!(server.join().must().len(), 8);
}

fn public_key(seed: u8) -> [u8; 32] {
    ed25519_dalek::SigningKey::from_bytes(&[seed; 32])
        .verifying_key()
        .to_bytes()
}

#[test]
fn receipt_keys_decode_height_ranges_and_refuse_invalid_entries() {
    let entry = |id: u8, key: [u8; 32], first: &str, last: &str| {
        let key = hex(&key);
        format!(
            r#"{{"keyId":"0x{}","publicKey":"0x{key}","firstHeight":"{first}","lastHeight":"{last}"}}"#,
            format!("{id:02x}").repeat(32)
        )
    };
    let listed = format!(
        r#""result":[{},{}]"#,
        entry(1, public_key(7), "0x1", "0x3e7"),
        entry(2, public_key(8), "0x3e8", "0xffffffffffffffff")
    );
    let (url, server) = serve(vec![
        ("px_getReceiptKeys", Some(listed)),
        (
            "px_getReceiptKeys",
            Some(format!(
                r#""result":[{},{}]"#,
                entry(1, public_key(7), "0x1", "0x3e7"),
                entry(1, public_key(8), "0x3e8", "0x7d0")
            )),
        ),
        (
            "px_getReceiptKeys",
            Some(format!(
                r#""result":[{}]"#,
                entry(1, public_key(7), "0x3e8", "0x3e7")
            )),
        ),
        (
            "px_getReceiptKeys",
            Some(format!(
                r#""result":[{}]"#,
                entry(0, public_key(7), "0x1", "0x2")
            )),
        ),
    ]);
    let endpoint = endpoint(&url);
    assert_eq!(
        endpoint.receipt_keys(),
        Ok(vec![
            ReceiptKeyEntry {
                key_id: [1; 32],
                public_key: public_key(7),
                first_height: 1,
                last_height: 999,
            },
            ReceiptKeyEntry {
                key_id: [2; 32],
                public_key: public_key(8),
                first_height: 1000,
                last_height: u64::MAX,
            },
        ])
    );
    for _ in 0..3 {
        assert_eq!(endpoint.receipt_keys(), Err(SequencerError::Malformed));
    }
    assert_eq!(server.join().must()[0], json!([]));
}

fn revert_data(reason: &str) -> String {
    let mut data = vec![0x08, 0xc3, 0x79, 0xa0];
    let mut offset = [0_u8; 32];
    offset[31] = 32;
    data.extend_from_slice(&offset);
    let mut length = [0_u8; 32];
    length[24..].copy_from_slice(&u64::try_from(reason.len()).must().to_be_bytes());
    data.extend_from_slice(&length);
    data.extend_from_slice(reason.as_bytes());
    data.resize(data.len() + (32 - reason.len() % 32) % 32, 0);
    hex(&data)
}

#[test]
fn bound_address_reads_the_addr_precompile_and_decodes_the_unbound_revert() {
    let did = [0x61_u8; 32];
    let unbound = "LXP_ADDR_DID_NOT_BOUND: LayerX DID is not bound to an EVM address; did:lx:abc";
    let other = "LXP_INVALID_ARGUMENT: argument is invalid";
    let (url, server) = serve(vec![
        (
            "eth_call",
            Some(
                r#""result":"0x00000000000000000000000011223344556677889900aabbccddeeff00112233""#
                    .to_owned(),
            ),
        ),
        (
            "eth_call",
            Some(format!(
                r#""error":{{"code":3,"message":"execution reverted: {unbound}","data":"0x{}"}}"#,
                revert_data(unbound)
            )),
        ),
        (
            "eth_call",
            Some(format!(
                r#""error":{{"code":3,"message":"execution reverted: {other}","data":"0x{}"}}"#,
                revert_data(other)
            )),
        ),
        (
            "eth_call",
            Some(
                r#""result":"0x0000000000000000000000000000000000000000000000000000000000000000""#
                    .to_owned(),
            ),
        ),
        ("eth_call", Some(r#""result":"0x1122""#.to_owned())),
    ]);
    let endpoint = endpoint(&url);
    assert_eq!(endpoint.bound_address(&did), Ok(Some(from())));
    assert_eq!(endpoint.bound_address(&did), Ok(None));
    assert_eq!(
        endpoint.bound_address(&did),
        Err(SequencerError::Rpc(RpcRefusal {
            code: 3,
            message: format!("execution reverted: {other}"),
            data: Some(Value::String(format!("0x{}", revert_data(other)))),
        }))
    );
    assert_eq!(endpoint.bound_address(&did), Err(SequencerError::Malformed));
    assert_eq!(endpoint.bound_address(&did), Err(SequencerError::Malformed));
    let params = server.join().must();
    assert_eq!(
        params[0],
        json!([{
            "to": "0x0000000000000000000000000000000000001004",
            "data": format!("0xc413bad3{}", "61".repeat(32)),
        }, "pending"])
    );
}

#[test]
fn pending_reads_and_send_use_the_pending_tag_and_exact_params() {
    let (url, server) = serve(vec![
        (
            "eth_getTransactionCount",
            Some(r#""result":"0x7""#.to_owned()),
        ),
        ("eth_estimateGas", Some(r#""result":"0x5208""#.to_owned())),
        ("eth_call", Some(r#""result":"0xbeef""#.to_owned())),
        (
            "eth_sendRawTransaction",
            Some(format!(r#""result":"{TX_HASH}""#)),
        ),
        (
            "eth_getTransactionCount",
            Some(r#""result":"0x""#.to_owned()),
        ),
    ]);
    let endpoint = endpoint(&url);
    assert_eq!(endpoint.pending_nonce(&from()), Ok(7));
    let call = CallRequest {
        from: Some(from()),
        to: Some([0x10; 20]),
        value: 10_u128.pow(19),
        data: vec![0xa9, 0x05],
    };
    assert_eq!(endpoint.estimate_gas(&call), Ok(21_000));
    assert_eq!(
        endpoint.call_pending(&[0x10; 20], &[0x01]),
        Ok(vec![0xbe, 0xef])
    );
    assert_eq!(
        endpoint.send_raw_transaction(&[0x02, 0xf8, 0x6b]),
        Ok(tx_hash())
    );
    assert_eq!(
        endpoint.pending_nonce(&from()),
        Err(SequencerError::Malformed)
    );
    let params = server.join().must();
    let sender = "0x11223344556677889900aabbccddeeff00112233";
    let target = format!("0x{}", "10".repeat(20));
    assert_eq!(params[0], json!([sender, "pending"]));
    assert_eq!(
        params[1],
        json!([{"from": sender, "to": target, "value": "0x8ac7230489e80000", "data": "0xa905"}, "pending"])
    );
    assert_eq!(
        params[2],
        json!([{"to": target, "data": "0x01"}, "pending"])
    );
    assert_eq!(params[3], json!(["0x02f86b"]));
}

#[test]
fn refusal_keeps_code_message_and_data() {
    let (url, server) = serve(vec![(
        "eth_sendRawTransaction",
        Some(r#""error":{"code":-32000,"message":"nonce too low: next nonce 8, tx nonce 7","data":{"next":"0x8","reason":["nonce",7]}}"#.to_owned()),
    ), (
        "eth_sendRawTransaction",
        Some(r#""error":{"code":-32003,"message":"sequencer unavailable","data":null}"#.to_owned()),
    )]);
    let endpoint = endpoint(&url);
    assert_eq!(
        endpoint.send_raw_transaction(&[0x02]),
        Err(SequencerError::Rpc(RpcRefusal {
            code: -32000,
            message: "nonce too low: next nonce 8, tx nonce 7".to_owned(),
            data: Some(json!({"next": "0x8", "reason": ["nonce", 7]})),
        }))
    );
    assert_eq!(
        endpoint.send_raw_transaction(&[0x02]),
        Err(SequencerError::Rpc(RpcRefusal {
            code: -32003,
            message: "sequencer unavailable".to_owned(),
            data: Some(Value::Null),
        }))
    );
    assert_eq!(server.join().must().len(), 2);
}

#[test]
fn closed_connection_is_unavailable() {
    let (url, server) = serve(vec![("eth_sendRawTransaction", None)]);
    assert_eq!(
        endpoint(&url).send_raw_transaction(&[0x02, 0xf8]),
        Err(SequencerError::Unavailable)
    );
    assert_eq!(server.join().must(), vec![json!(["0x02f8"])]);
    let listener = TcpListener::bind("127.0.0.1:0").must();
    let closed = format!("http://127.0.0.1:{}/", listener.local_addr().must().port());
    drop(listener);
    assert_eq!(
        endpoint(&closed).pending_nonce(&from()),
        Err(SequencerError::Unavailable)
    );
}

#[test]
fn unusable_send_answer_is_unavailable() {
    let (url, server) = serve(vec![
        (
            "eth_sendRawTransaction",
            Some(r#""result":"0x""#.to_owned()),
        ),
        (
            "eth_sendRawTransaction",
            Some(format!(r#""result":"{TX_HASH}","extra":true"#)),
        ),
        (
            "eth_sendRawTransaction",
            Some(r#""error":{"code":-32000,"message":"nonce too low","extra":true}"#.to_owned()),
        ),
        ("eth_call", Some(r#""result":"0x0""#.to_owned())),
    ]);
    let endpoint = endpoint(&url);
    for _ in 0..3 {
        assert_eq!(
            endpoint.send_raw_transaction(&[0x02, 0xf8]),
            Err(SequencerError::Unavailable)
        );
    }
    assert_eq!(
        endpoint.call_pending(&[0x10; 20], &[0x01]),
        Err(SequencerError::Malformed)
    );
    assert_eq!(server.join().must().len(), 4);
}

#[test]
fn endpoint_refuses_plain_remote_urls_and_unpinned_https() {
    assert_eq!(
        SequencerEndpoint::new("http://router.example/", &[]).map(|_| ()),
        Err(SequencerError::Endpoint)
    );
    assert_eq!(
        SequencerEndpoint::new("https://router.example/", &[]).map(|_| ()),
        Err(SequencerError::Endpoint)
    );
    assert_eq!(
        SequencerEndpoint::new("https://router.example/", &[&[]]).map(|_| ()),
        Err(SequencerError::Endpoint)
    );
    assert_eq!(
        SequencerEndpoint::new("ftp://127.0.0.1/", &[]).map(|_| ()),
        Err(SequencerError::Endpoint)
    );
    assert!(SequencerEndpoint::new("https://router.example/", &[&[0x30, 0x82]]).is_ok());
}

const BLOCK_HASH: &str = "0x4b1f2c9d8e7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b7c6d5e4f3a2b1c";
const PARENT_HASH: &str = "0x9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b3c2d1e0f9a8b";
const TRANSACTIONS_ROOT: &str =
    "0x1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b1c2d";

/// The EIP-155 example: nonce 9, 20 gwei, 21000 gas, 1 ether to 0x3535..35
/// on chain 1, signed with the key 0x4646..46.
const LEGACY_RAW: &str = "f86c098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a76400008025a028ef61340bd939bc2195fe537567866003e1a15d3c71ff63e1590620aa636276a067cbe9d8997f761aecb703304b3800ccf555c9f3dc64214b297fb1966a3b6d83";

fn unhex(text: &str) -> Vec<u8> {
    (0..text.len())
        .step_by(2)
        .map(|index| u8::from_str_radix(&text[index..index + 2], 16).must())
        .collect()
}

fn quantity(bytes: &[u8]) -> String {
    let digits = hex(bytes);
    let trimmed = digits.trim_start_matches('0');
    if trimmed.is_empty() {
        "0x0".to_owned()
    } else {
        format!("0x{trimmed}")
    }
}

fn location(index: u64) -> String {
    format!(r#""blockHash":"{BLOCK_HASH}","blockNumber":"0x3e8","transactionIndex":"{index:#x}""#)
}

fn legacy_transaction(index: u64) -> String {
    format!(
        r#"{{{},"from":"0x9d8a62f656a8d1615c1294fd71e9cfb3e4855a4f","gas":"0x5208","gasPrice":"0x4a817c800","hash":"0x33469b22e9f636356c4160a87eb19df52b7412e8eac32a4a55ffe88ea8350788","input":"0x","nonce":"0x9","to":"0x3535353535353535353535353535353535353535","value":"0xde0b6b3a7640000","type":"0x0","chainId":"0x1","v":"0x25","r":"0x28ef61340bd939bc2195fe537567866003e1a15d3c71ff63e1590620aa636276","s":"0x67cbe9d8997f761aecb703304b3800ccf555c9f3dc64214b297fb1966a3b6d83"}}"#,
        location(index)
    )
}

/// A real EIP-1559 call signed with the key 0x4646..46 and its RPC object.
fn dynamic_fee_transaction(index: u64) -> (Vec<u8>, String) {
    let call = Eip1559Call {
        chain_id: 229,
        nonce: 3,
        max_priority_fee_per_gas: 1_000_000_000,
        max_fee_per_gas: 3_000_000_000,
        gas_limit: 90_000,
        to: [
            0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10, 0x04,
        ],
        data: vec![0xc4, 0x13, 0xba, 0xd3, 0x01, 0x02],
    };
    let raw = call.sign(&[0x46; 32]).must();
    let tail = raw.len() - 67;
    assert_eq!(raw[tail + 1], 0xa0);
    assert_eq!(raw[tail + 34], 0xa0);
    let y_parity = match raw[tail] {
        0x80 => "0x0",
        0x01 => "0x1",
        other => panic!("y parity byte {other:#x}"),
    };
    let object = format!(
        r#"{{{},"from":"0x9d8a62f656a8d1615c1294fd71e9cfb3e4855a4f","gas":"0x15f90","gasPrice":"0x77359400","maxFeePerGas":"0xb2d05e00","maxPriorityFeePerGas":"0x3b9aca00","hash":"0x{}","input":"0xc413bad30102","nonce":"0x3","to":"0x0000000000000000000000000000000000001004","value":"0x0","type":"0x2","accessList":[],"chainId":"0xe5","v":"{y_parity}","r":"{}","s":"{}","yParity":"{y_parity}"}}"#,
        location(index),
        "5e".repeat(32),
        quantity(&raw[tail + 2..tail + 34]),
        quantity(&raw[tail + 35..]),
    );
    (raw, object)
}

fn block(transactions: &str) -> String {
    format!(
        r#""result":{{"number":"0x3e8","hash":"{BLOCK_HASH}","parentHash":"{PARENT_HASH}","timestamp":"0x67a1b2c3","transactionsRoot":"{TRANSACTIONS_ROOT}","gasLimit":"0x1c9c380","gasUsed":"0x1a9f8","miner":"0x0000000000000000000000000000000000000000","transactions":[{transactions}]}}"#
    )
}

#[test]
fn full_block_decodes_raw_transactions_in_index_order() {
    let (dynamic_raw, dynamic) = dynamic_fee_transaction(1);
    let (url, server) = serve(vec![(
        "eth_getBlockByNumber",
        Some(block(&format!("{},{dynamic}", legacy_transaction(0)))),
    )]);
    assert_eq!(
        endpoint(&url).block_by_number(1000),
        Ok(Some(SequencerBlock {
            number: 1000,
            hash: decode32(BLOCK_HASH),
            parent_hash: decode32(PARENT_HASH),
            timestamp: 0x67a1_b2c3,
            transactions_root: decode32(TRANSACTIONS_ROOT),
            transactions: vec![unhex(LEGACY_RAW), dynamic_raw],
        }))
    );
    assert_eq!(server.join().must(), vec![json!(["0x3e8", true])]);
}

#[test]
fn null_block_is_none() {
    let (url, server) = serve(vec![(
        "eth_getBlockByNumber",
        Some(r#""result":null"#.to_owned()),
    )]);
    assert_eq!(endpoint(&url).block_by_number(0), Ok(None));
    assert_eq!(server.join().must(), vec![json!(["0x0", true])]);
}

#[test]
fn malformed_transaction_lists_are_refused() {
    let (_, dynamic) = dynamic_fee_transaction(1);
    let legacy = legacy_transaction(0);
    let malformed = vec![
        block(r#""0x33469b22e9f636356c4160a87eb19df52b7412e8eac32a4a55ffe88ea8350788""#),
        block(&format!("{dynamic},{legacy}")),
        block(&legacy.replace(r#""v":"0x25""#, r#""v":"0x1b""#)),
        block(&format!(
            "{legacy},{}",
            dynamic.replace(r#","yParity":"#, r#","maxFeePerBlobGas":"0x1","yParity":"#)
        )),
        block(&format!(
            "{legacy},{}",
            dynamic.replace(r#""type":"0x2""#, r#""type":"0x5""#)
        )),
        block(&legacy.replace(BLOCK_HASH, PARENT_HASH)),
        r#""result":{"number":"0x3e8"}"#.to_owned(),
        block(&legacy).replace(r#""number":"0x3e8""#, r#""number":"0x3e9""#),
    ];
    let count = malformed.len();
    let (url, server) = serve(
        malformed
            .into_iter()
            .map(|members| ("eth_getBlockByNumber", Some(members)))
            .collect(),
    );
    let endpoint = endpoint(&url);
    for _ in 0..count {
        assert_eq!(
            endpoint.block_by_number(1000),
            Err(SequencerError::Malformed)
        );
    }
    assert_eq!(server.join().must().len(), count);
}

#[test]
fn closed_connection_during_block_read_is_unavailable() {
    let (url, server) = serve(vec![("eth_getBlockByNumber", None)]);
    assert_eq!(
        endpoint(&url).block_by_number(1000),
        Err(SequencerError::Unavailable)
    );
    assert_eq!(server.join().must(), vec![json!(["0x3e8", true])]);
}

#[test]
fn call_at_sends_the_explicit_height_and_keeps_a_revert_at_that_height() {
    let reason = revert_data("LXP_ADDR_DID_NOT_BOUND: no binding at this height");
    let (url, server) = serve(vec![
        ("eth_call", Some(r#""result":"0x00ff10""#.to_owned())),
        (
            "eth_call",
            Some(format!(
                r#""error":{{"code":3,"message":"execution reverted","data":"0x{reason}"}}"#
            )),
        ),
    ]);
    let endpoint = endpoint(&url);
    let mut to = [0_u8; 20];
    to[18] = 0x10;
    to[19] = 0x04;
    assert_eq!(
        endpoint.call_at(1000, &to, &[0xc4, 0x13, 0xba, 0xd3]),
        Ok(vec![0x00, 0xff, 0x10])
    );
    assert_eq!(
        endpoint.call_at(0, &to, &[]),
        Err(SequencerError::Rpc(RpcRefusal {
            code: 3,
            message: "execution reverted".to_owned(),
            data: Some(Value::String(format!("0x{reason}"))),
        }))
    );
    assert_eq!(
        server.join().must(),
        vec![
            json!([{"to": "0x0000000000000000000000000000000000001004", "data": "0xc413bad3"}, "0x3e8"]),
            json!([{"to": "0x0000000000000000000000000000000000001004", "data": "0x"}, "0x0"]),
        ]
    );
}
