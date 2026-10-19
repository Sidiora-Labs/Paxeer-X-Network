//! Cosmos transaction of the price sidecar: the protobuf encoding of one
//! `MsgSubmitPrices` in a sign-mode-direct `TxRaw`, byte-identical to the Go
//! encoding, signed with the secp256k1 feed key.

use k256::ecdsa::signature::hazmat::PrehashSigner;
use k256::ecdsa::{Signature, SigningKey};
use sha2::{Digest, Sha256};

/// Bech32 prefix of account addresses (`AccountAddressPrefix`).
pub const ACCOUNT_PREFIX: &str = "pax";
/// Type URL of `paxprotocol.paxchain.pricefeed.MsgSubmitPrices`.
pub const MSG_SUBMIT_PRICES_TYPE_URL: &str = "/paxprotocol.paxchain.pricefeed.MsgSubmitPrices";
/// Type URL of `cosmos.crypto.secp256k1.PubKey`.
pub const SECP256K1_PUBKEY_TYPE_URL: &str = "/cosmos.crypto.secp256k1.PubKey";
/// `SIGN_MODE_DIRECT`.
pub const SIGN_MODE_DIRECT: u64 = 1;
/// Most observations `ValidateBasic` accepts in one `MsgSubmitPrices`.
pub const MAX_OBSERVATIONS: usize = 64;

const WIRE_VARINT: u8 = 0;
const WIRE_LEN: u8 = 2;
const BECH32_CHARSET: &[u8; 32] = b"qpzry9x8gf2tvdw0s3jn54khce6mua7l";
const BECH32_GENERATOR: [u32; 5] = [
    0x3b6a_57b2,
    0x2650_8e6d,
    0x1ea1_19fa,
    0x3d42_33dd,
    0x2a14_62b3,
];

fn put_varint(out: &mut Vec<u8>, mut value: u64) {
    while value >= 0x80 {
        out.push((value.to_le_bytes()[0] & 0x7f) | 0x80);
        value >>= 7;
    }
    out.push(value.to_le_bytes()[0]);
}

fn put_key(out: &mut Vec<u8>, field: u32, wire: u8) {
    put_varint(out, (u64::from(field) << 3) | u64::from(wire));
}

fn put_uint(out: &mut Vec<u8>, field: u32, value: u64) {
    if value != 0 {
        put_key(out, field, WIRE_VARINT);
        put_varint(out, value);
    }
}

fn put_len(out: &mut Vec<u8>, field: u32, bytes: &[u8]) {
    put_key(out, field, WIRE_LEN);
    put_varint(out, bytes.len() as u64);
    out.extend_from_slice(bytes);
}

fn put_scalar_bytes(out: &mut Vec<u8>, field: u32, bytes: &[u8]) {
    if !bytes.is_empty() {
        put_len(out, field, bytes);
    }
}

/// One market's prices, prices as 18-decimal integer strings.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Observation {
    pub market_id: u64,
    pub sequence: u64,
    pub index_price: String,
    pub mark_price: String,
    pub source_timestamp_ms: u64,
}

impl Observation {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_uint(&mut out, 1, self.market_id);
        put_uint(&mut out, 2, self.sequence);
        put_scalar_bytes(&mut out, 3, self.index_price.as_bytes());
        put_scalar_bytes(&mut out, 4, self.mark_price.as_bytes());
        put_uint(&mut out, 5, self.source_timestamp_ms);
        out
    }
}

/// `MsgSubmitPrices`: the bech32 feed account and its observations.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct MsgSubmitPrices {
    pub feeder: String,
    pub observations: Vec<Observation>,
}

impl MsgSubmitPrices {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_scalar_bytes(&mut out, 1, self.feeder.as_bytes());
        for observation in &self.observations {
            put_len(&mut out, 2, &observation.encode());
        }
        out
    }
}

/// `google.protobuf.Any`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Any {
    pub type_url: String,
    pub value: Vec<u8>,
}

impl Any {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_scalar_bytes(&mut out, 1, self.type_url.as_bytes());
        put_scalar_bytes(&mut out, 2, &self.value);
        out
    }
}

/// `TxBody` without memo, timeout or extension options.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TxBody {
    pub messages: Vec<Any>,
}

impl TxBody {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        for message in &self.messages {
            put_len(&mut out, 1, &message.encode());
        }
        out
    }
}

/// `SignerInfo` of a single secp256k1 key in sign mode direct.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignerInfo {
    pub public_key: [u8; 33],
    pub sequence: u64,
}

impl SignerInfo {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut key = Vec::with_capacity(35);
        put_len(&mut key, 1, &self.public_key);
        let public_key = Any {
            type_url: SECP256K1_PUBKEY_TYPE_URL.to_owned(),
            value: key,
        };
        let mut single = Vec::new();
        put_uint(&mut single, 1, SIGN_MODE_DIRECT);
        let mut mode_info = Vec::new();
        put_len(&mut mode_info, 1, &single);

        let mut out = Vec::new();
        put_len(&mut out, 1, &public_key.encode());
        put_len(&mut out, 2, &mode_info);
        put_uint(&mut out, 3, self.sequence);
        out
    }
}

/// `AuthInfo` with one signer and a fee of no coins at `gas_limit`.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AuthInfo {
    pub signer_info: SignerInfo,
    pub gas_limit: u64,
}

impl AuthInfo {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut fee = Vec::new();
        put_uint(&mut fee, 2, self.gas_limit);
        let mut out = Vec::new();
        put_len(&mut out, 1, &self.signer_info.encode());
        put_len(&mut out, 2, &fee);
        out
    }
}

/// `SignDoc` of sign mode direct.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct SignDoc {
    pub body_bytes: Vec<u8>,
    pub auth_info_bytes: Vec<u8>,
    pub chain_id: String,
    pub account_number: u64,
}

impl SignDoc {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_scalar_bytes(&mut out, 1, &self.body_bytes);
        put_scalar_bytes(&mut out, 2, &self.auth_info_bytes);
        put_scalar_bytes(&mut out, 3, self.chain_id.as_bytes());
        put_uint(&mut out, 4, self.account_number);
        out
    }
}

/// `TxRaw`, the broadcast form.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct TxRaw {
    pub body_bytes: Vec<u8>,
    pub auth_info_bytes: Vec<u8>,
    pub signatures: Vec<Vec<u8>>,
}

impl TxRaw {
    #[must_use]
    pub fn encode(&self) -> Vec<u8> {
        let mut out = Vec::new();
        put_scalar_bytes(&mut out, 1, &self.body_bytes);
        put_scalar_bytes(&mut out, 2, &self.auth_info_bytes);
        for signature in &self.signatures {
            put_len(&mut out, 3, signature);
        }
        out
    }
}

fn signing_key(secret: &[u8; 32]) -> Result<SigningKey, String> {
    SigningKey::from_slice(secret)
        .map_err(|_| "feed key is not a valid secp256k1 secret".to_owned())
}

/// The 33-byte compressed secp256k1 public key of `secret`.
///
/// # Errors
/// Refuses a secret that is zero or not below the curve order.
pub fn compressed_public_key(secret: &[u8; 32]) -> Result<[u8; 33], String> {
    let point = signing_key(secret)?.verifying_key().to_encoded_point(true);
    point
        .as_bytes()
        .try_into()
        .map_err(|_| "compressed public key is not 33 bytes".to_owned())
}

/// The 64-byte low-s `r || s` signature of `sha256(doc)`.
///
/// # Errors
/// Refuses an invalid secret or a failed signature.
pub fn sign_doc(secret: &[u8; 32], doc: &SignDoc) -> Result<[u8; 64], String> {
    let digest: [u8; 32] = Sha256::digest(doc.encode()).into();
    let signature: Signature = signing_key(secret)?
        .sign_prehash(&digest)
        .map_err(|_| "feed key signing failed".to_owned())?;
    let signature = signature.normalize_s().unwrap_or(signature);
    Ok(signature.to_bytes().into())
}

fn bech32_polymod(values: impl IntoIterator<Item = u8>) -> u32 {
    let mut checksum: u32 = 1;
    for value in values {
        let top = checksum >> 25;
        checksum = ((checksum & 0x01ff_ffff) << 5) ^ u32::from(value);
        for (bit, generator) in BECH32_GENERATOR.iter().enumerate() {
            if (top >> bit) & 1 == 1 {
                checksum ^= generator;
            }
        }
    }
    checksum
}

fn bech32_encode(hrp: &str, data: &[u8]) -> String {
    let mut words = Vec::with_capacity(data.len() * 8 / 5 + 1);
    let mut accumulator: u32 = 0;
    let mut bits: u32 = 0;
    for byte in data {
        accumulator = (accumulator << 8) | u32::from(*byte);
        bits += 8;
        while bits >= 5 {
            bits -= 5;
            words.push((accumulator >> bits).to_le_bytes()[0] & 31);
        }
    }
    if bits > 0 {
        words.push((accumulator << (5 - bits)).to_le_bytes()[0] & 31);
    }
    let expanded = hrp
        .bytes()
        .map(|c| c >> 5)
        .chain(std::iter::once(0))
        .chain(hrp.bytes().map(|c| c & 31));
    let checksum = bech32_polymod(expanded.chain(words.iter().copied()).chain([0; 6])) ^ 1;
    let mut out = String::with_capacity(hrp.len() + 1 + words.len() + 6);
    out.push_str(hrp);
    out.push('1');
    for word in words
        .iter()
        .copied()
        .chain((0..6).map(|i: u32| (checksum >> (5 * (5 - i))).to_le_bytes()[0] & 31))
    {
        out.push(char::from(BECH32_CHARSET[usize::from(word)]));
    }
    out
}

/// The bech32 `pax` address of the 20-byte feed account.
#[must_use]
pub fn feed_account(account: &[u8; 20]) -> String {
    bech32_encode(ACCOUNT_PREFIX, account)
}

/// Everything one submission transaction carries besides the feed key.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Submission {
    pub chain_id: String,
    pub account_number: u64,
    pub sequence: u64,
    pub gas_limit: u64,
    pub feed_account: [u8; 20],
    pub observations: Vec<Observation>,
}

/// Encodes and signs the `TxRaw` bytes of one `MsgSubmitPrices`.
///
/// # Errors
/// Refuses an empty chain id, no observations, more than
/// [`MAX_OBSERVATIONS`] observations, and the refusals of [`sign_doc`].
pub fn encode_tx(secret: &[u8; 32], submission: &Submission) -> Result<Vec<u8>, String> {
    if submission.chain_id.is_empty() {
        return Err("chain id is empty".to_owned());
    }
    if submission.observations.is_empty() {
        return Err("submission has no observations".to_owned());
    }
    if submission.observations.len() > MAX_OBSERVATIONS {
        return Err(format!(
            "submission has {} observations, at most {MAX_OBSERVATIONS}",
            submission.observations.len()
        ));
    }
    let message = MsgSubmitPrices {
        feeder: feed_account(&submission.feed_account),
        observations: submission.observations.clone(),
    };
    let body = TxBody {
        messages: vec![Any {
            type_url: MSG_SUBMIT_PRICES_TYPE_URL.to_owned(),
            value: message.encode(),
        }],
    };
    let auth_info = AuthInfo {
        signer_info: SignerInfo {
            public_key: compressed_public_key(secret)?,
            sequence: submission.sequence,
        },
        gas_limit: submission.gas_limit,
    };
    let doc = SignDoc {
        body_bytes: body.encode(),
        auth_info_bytes: auth_info.encode(),
        chain_id: submission.chain_id.clone(),
        account_number: submission.account_number,
    };
    let signature = sign_doc(secret, &doc)?;
    Ok(TxRaw {
        body_bytes: doc.body_bytes,
        auth_info_bytes: doc.auth_info_bytes,
        signatures: vec![signature.to_vec()],
    }
    .encode())
}

#[cfg(test)]
mod tests {
    use super::*;
    use k256::ecdsa::signature::hazmat::PrehashVerifier;
    use k256::ecdsa::VerifyingKey;

    const SECRET: [u8; 32] = [1; 32];
    const HALF_ORDER: [u8; 32] = [
        0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
        0xff, 0x5d, 0x57, 0x6e, 0x73, 0x57, 0xa4, 0x50, 0x1d, 0xdf, 0xe9, 0x2f, 0x46, 0x68, 0x1b,
        0x20, 0xa0,
    ];

    fn observation() -> Observation {
        Observation {
            market_id: 1,
            sequence: 300,
            index_price: "12".to_owned(),
            mark_price: "13".to_owned(),
            source_timestamp_ms: 5,
        }
    }

    fn with_len(field_key: u8, body: &[u8]) -> Vec<u8> {
        let mut out = vec![field_key, u8::try_from(body.len()).unwrap_or(u8::MAX)];
        out.extend_from_slice(body);
        out
    }

    #[test]
    fn varint_encodes_multibyte_values() {
        let mut out = Vec::new();
        put_varint(&mut out, 300);
        put_varint(&mut out, u64::MAX);
        assert_eq!(
            out,
            [0xac, 0x02, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01]
        );
    }

    #[test]
    fn observation_encodes_fields_in_order_and_omits_zeros() {
        assert_eq!(
            observation().encode(),
            [
                0x08, 0x01, 0x10, 0xac, 0x02, 0x1a, 0x02, b'1', b'2', 0x22, 0x02, b'1', b'3', 0x28,
                0x05
            ]
        );
        let zero = Observation {
            market_id: 0,
            sequence: 1,
            index_price: "7".to_owned(),
            mark_price: "7".to_owned(),
            source_timestamp_ms: 2,
        };
        assert_eq!(
            zero.encode(),
            [0x10, 0x01, 0x1a, 0x01, b'7', 0x22, 0x01, b'7', 0x28, 0x02]
        );
    }

    #[test]
    fn msg_submit_prices_repeats_observations() {
        let encoded_observation = observation().encode();
        let message = MsgSubmitPrices {
            feeder: "pax1x".to_owned(),
            observations: vec![observation(), observation()],
        };
        let mut expected = with_len(0x0a, b"pax1x");
        expected.extend(with_len(0x12, &encoded_observation));
        expected.extend(with_len(0x12, &encoded_observation));
        assert_eq!(message.encode(), expected);
    }

    #[test]
    fn any_and_body_wrap_the_message() {
        let any = Any {
            type_url: "/a.B".to_owned(),
            value: vec![0x08, 0x01],
        };
        assert_eq!(
            any.encode(),
            [0x0a, 0x04, b'/', b'a', b'.', b'B', 0x12, 0x02, 0x08, 0x01]
        );
        let body = TxBody {
            messages: vec![any.clone()],
        };
        assert_eq!(body.encode(), with_len(0x0a, &any.encode()));
    }

    #[test]
    fn signer_info_and_auth_info_match_sign_mode_direct_layout() {
        let public_key = [2; 33];
        let info = SignerInfo {
            public_key,
            sequence: 3,
        };
        let mut key = vec![0x0a, 0x21];
        key.extend_from_slice(&public_key);
        let mut any = with_len(0x0a, SECP256K1_PUBKEY_TYPE_URL.as_bytes());
        any.extend(with_len(0x12, &key));
        let mut expected = with_len(0x0a, &any);
        expected.extend([0x12, 0x04, 0x0a, 0x02, 0x08, 0x01, 0x18, 0x03]);
        assert_eq!(info.encode(), expected);

        let first = SignerInfo {
            public_key,
            sequence: 0,
        };
        assert_eq!(first.encode(), expected[..expected.len() - 2]);

        let auth = AuthInfo {
            signer_info: info.clone(),
            gas_limit: 200_000,
        };
        let mut expected_auth = with_len(0x0a, &info.encode());
        expected_auth.extend([0x12, 0x04, 0x10, 0xc0, 0x9a, 0x0c]);
        assert_eq!(auth.encode(), expected_auth);

        let no_gas = AuthInfo {
            signer_info: info.clone(),
            gas_limit: 0,
        };
        let mut expected_no_gas = with_len(0x0a, &info.encode());
        expected_no_gas.extend([0x12, 0x00]);
        assert_eq!(no_gas.encode(), expected_no_gas);
    }

    #[test]
    fn sign_doc_and_tx_raw_encode_fields() {
        let doc = SignDoc {
            body_bytes: vec![0xaa],
            auth_info_bytes: vec![0xbb, 0xcc],
            chain_id: "test-1".to_owned(),
            account_number: 9,
        };
        assert_eq!(
            doc.encode(),
            [
                0x0a, 0x01, 0xaa, 0x12, 0x02, 0xbb, 0xcc, 0x1a, 0x06, b't', b'e', b's', b't', b'-',
                b'1', 0x20, 0x09
            ]
        );
        let raw = TxRaw {
            body_bytes: vec![0xaa],
            auth_info_bytes: vec![0xbb],
            signatures: vec![vec![0x01, 0x02]],
        };
        assert_eq!(
            raw.encode(),
            [0x0a, 0x01, 0xaa, 0x12, 0x01, 0xbb, 0x1a, 0x02, 0x01, 0x02]
        );
    }

    #[test]
    fn compressed_public_key_of_one_is_the_generator() {
        let mut one = [0; 32];
        one[31] = 1;
        let expected = [
            0x02, 0x79, 0xbe, 0x66, 0x7e, 0xf9, 0xdc, 0xbb, 0xac, 0x55, 0xa0, 0x62, 0x95, 0xce,
            0x87, 0x0b, 0x07, 0x02, 0x9b, 0xfc, 0xdb, 0x2d, 0xce, 0x28, 0xd9, 0x59, 0xf2, 0x81,
            0x5b, 0x16, 0xf8, 0x17, 0x98,
        ];
        assert_eq!(compressed_public_key(&one), Ok(expected));
        assert_eq!(
            compressed_public_key(&[0; 32]),
            Err("feed key is not a valid secp256k1 secret".to_owned())
        );
        assert_eq!(
            compressed_public_key(&[0xff; 32]),
            Err("feed key is not a valid secp256k1 secret".to_owned())
        );
    }

    #[test]
    fn signature_is_low_s_and_verifies() -> Result<(), String> {
        let doc = SignDoc {
            body_bytes: vec![0xaa],
            auth_info_bytes: vec![0xbb],
            chain_id: "test-1".to_owned(),
            account_number: 9,
        };
        let signature = sign_doc(&SECRET, &doc)?;
        assert!(signature[32..] <= HALF_ORDER[..]);
        assert_eq!(sign_doc(&SECRET, &doc)?, signature);
        let public_key = compressed_public_key(&SECRET)?;
        let verifying =
            VerifyingKey::from_sec1_bytes(&public_key).map_err(|error| error.to_string())?;
        let parsed = Signature::from_slice(&signature).map_err(|error| error.to_string())?;
        let digest = Sha256::digest(doc.encode());
        verifying
            .verify_prehash(&digest, &parsed)
            .map_err(|error| error.to_string())?;
        let other = SignDoc {
            account_number: 10,
            ..doc
        };
        assert!(verifying
            .verify_prehash(&Sha256::digest(other.encode()), &parsed)
            .is_err());
        Ok(())
    }

    #[test]
    fn feed_account_is_bech32_with_the_pax_prefix() {
        assert_eq!(
            feed_account(&[1; 20]),
            "pax1qyqszqgpqyqszqgpqyqszqgpqyqszqgpwuux75"
        );
        let mut counting = [0; 20];
        for (index, byte) in counting.iter_mut().enumerate() {
            *byte = u8::try_from(index).unwrap_or(u8::MAX);
        }
        assert_eq!(
            feed_account(&counting),
            "pax1qqqsyqcyq5rqwzqfpg9scrgwpugpzysnlevzz3"
        );
        let reference = [
            0x00, 0x44, 0x32, 0x14, 0xc7, 0x42, 0x54, 0xb6, 0x35, 0xcf, 0x84, 0x65, 0x3a, 0x56,
            0xd7, 0xc6, 0x75, 0xbe, 0x77, 0xdf,
        ];
        assert_eq!(
            bech32_encode("abcdef", &reference),
            "abcdef1qpzry9x8gf2tvdw0s3jn54khce6mua7lmqqqxw"
        );
    }

    fn submission(observations: Vec<Observation>) -> Submission {
        Submission {
            chain_id: "test-1".to_owned(),
            account_number: 9,
            sequence: 3,
            gas_limit: 200_000,
            feed_account: [1; 20],
            observations,
        }
    }

    #[test]
    fn encode_tx_assembles_a_signed_tx_raw() -> Result<(), String> {
        let input = submission(vec![observation()]);
        let tx = encode_tx(&SECRET, &input)?;
        let message = MsgSubmitPrices {
            feeder: "pax1qyqszqgpqyqszqgpqyqszqgpqyqszqgpwuux75".to_owned(),
            observations: vec![observation()],
        };
        let body = TxBody {
            messages: vec![Any {
                type_url: MSG_SUBMIT_PRICES_TYPE_URL.to_owned(),
                value: message.encode(),
            }],
        }
        .encode();
        let auth_info = AuthInfo {
            signer_info: SignerInfo {
                public_key: compressed_public_key(&SECRET)?,
                sequence: 3,
            },
            gas_limit: 200_000,
        }
        .encode();
        let doc = SignDoc {
            body_bytes: body.clone(),
            auth_info_bytes: auth_info.clone(),
            chain_id: "test-1".to_owned(),
            account_number: 9,
        };
        let expected = TxRaw {
            body_bytes: body,
            auth_info_bytes: auth_info,
            signatures: vec![sign_doc(&SECRET, &doc)?.to_vec()],
        }
        .encode();
        assert_eq!(tx, expected);
        Ok(())
    }

    #[test]
    fn encode_tx_refuses_out_of_bounds_input() {
        assert_eq!(
            encode_tx(&SECRET, &submission(Vec::new())),
            Err("submission has no observations".to_owned())
        );
        assert_eq!(
            encode_tx(
                &SECRET,
                &submission(vec![observation(); MAX_OBSERVATIONS + 1])
            ),
            Err("submission has 65 observations, at most 64".to_owned())
        );
        assert!(encode_tx(&SECRET, &submission(vec![observation(); MAX_OBSERVATIONS])).is_ok());
        let mut no_chain = submission(vec![observation()]);
        no_chain.chain_id.clear();
        assert_eq!(
            encode_tx(&SECRET, &no_chain),
            Err("chain id is empty".to_owned())
        );
        assert_eq!(
            encode_tx(&[0; 32], &submission(vec![observation()])),
            Err("feed key is not a valid secp256k1 secret".to_owned())
        );
    }
}
