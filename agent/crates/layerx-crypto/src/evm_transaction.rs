//! EIP-1559 call signing for the one transaction an account sends itself:
//! binding its `LayerX` identity through the `addr` precompile.

use k256::ecdsa::{RecoveryId, Signature, SigningKey, VerifyingKey};
use sha3::{Digest as _, Keccak256};

use crate::secp256k1::evm_address;

/// An unsigned EIP-1559 call carrying no value and no access list.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Eip1559Call {
    pub chain_id: u64,
    pub nonce: u64,
    pub max_priority_fee_per_gas: u128,
    pub max_fee_per_gas: u128,
    pub gas_limit: u64,
    pub to: [u8; 20],
    pub data: Vec<u8>,
}

/// The private key is not a usable secp256k1 scalar.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct InvalidEvmKey;

impl std::fmt::Display for InvalidEvmKey {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str("invalid_evm_key")
    }
}

impl std::error::Error for InvalidEvmKey {}

fn rlp_length(length: usize, short: u8, out: &mut Vec<u8>) {
    if length < 56 {
        #[allow(clippy::cast_possible_truncation)]
        out.push(short + length as u8);
    } else {
        let bytes = length.to_be_bytes();
        let skip = bytes.iter().take_while(|byte| **byte == 0).count();
        #[allow(clippy::cast_possible_truncation)]
        out.push(short + 55 + (bytes.len() - skip) as u8);
        out.extend_from_slice(&bytes[skip..]);
    }
}

fn rlp_bytes(bytes: &[u8], out: &mut Vec<u8>) {
    if bytes.len() == 1 && bytes[0] < 0x80 {
        out.push(bytes[0]);
    } else {
        rlp_length(bytes.len(), 0x80, out);
        out.extend_from_slice(bytes);
    }
}

fn rlp_integer(bytes: &[u8], out: &mut Vec<u8>) {
    let skip = bytes.iter().take_while(|byte| **byte == 0).count();
    rlp_bytes(&bytes[skip..], out);
}

fn rlp_list(payload: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(payload.len() + 9);
    rlp_length(payload.len(), 0xc0, &mut out);
    out.extend_from_slice(payload);
    out
}

impl Eip1559Call {
    fn fields(&self) -> Vec<u8> {
        let mut out = Vec::with_capacity(self.data.len() + 96);
        rlp_integer(&self.chain_id.to_be_bytes(), &mut out);
        rlp_integer(&self.nonce.to_be_bytes(), &mut out);
        rlp_integer(&self.max_priority_fee_per_gas.to_be_bytes(), &mut out);
        rlp_integer(&self.max_fee_per_gas.to_be_bytes(), &mut out);
        rlp_integer(&self.gas_limit.to_be_bytes(), &mut out);
        rlp_bytes(&self.to, &mut out);
        rlp_integer(&[], &mut out);
        rlp_bytes(&self.data, &mut out);
        out.push(0xc0);
        out
    }

    /// The digest the sender signs.
    #[must_use]
    pub fn signing_hash(&self) -> [u8; 32] {
        let mut hasher = Keccak256::new();
        hasher.update([0x02]);
        hasher.update(rlp_list(&self.fields()));
        hasher.finalize().into()
    }

    /// Signs the call, yielding the bytes `eth_sendRawTransaction` takes.
    ///
    /// # Errors
    ///
    /// Refuses a key that is zero or not below the group order.
    pub fn sign(&self, evm_secret: &[u8; 32]) -> Result<Vec<u8>, InvalidEvmKey> {
        let key = SigningKey::from_slice(evm_secret).map_err(|_| InvalidEvmKey)?;
        let (signature, recovery) = key
            .sign_prehash_recoverable(&self.signing_hash())
            .map_err(|_| InvalidEvmKey)?;
        let address = evm_address(key.verifying_key().to_encoded_point(false).as_bytes())
            .map_err(|_| InvalidEvmKey)?;
        self.assemble(&signature.to_bytes().into(), recovery.to_byte(), &address)
    }

    /// Assembles the signed call from a signature produced elsewhere,
    /// yielding the bytes `eth_sendRawTransaction` takes.
    ///
    /// # Errors
    ///
    /// Refuses a recovery id other than 0 or 1, a signature whose scalars are
    /// zero or not below the group order, a high-s signature, a signature that
    /// recovers no key over this call's digest, and a recovered sender that is
    /// not `expected_address`.
    pub fn assemble(
        &self,
        signature: &[u8; 64],
        recovery_id: u8,
        expected_address: &[u8; 20],
    ) -> Result<Vec<u8>, InvalidEvmKey> {
        let recovery = RecoveryId::try_from(recovery_id)
            .ok()
            .filter(|value| value.to_byte() <= 1)
            .ok_or(InvalidEvmKey)?;
        let parsed = Signature::from_slice(signature).map_err(|_| InvalidEvmKey)?;
        if parsed.normalize_s().is_some() {
            return Err(InvalidEvmKey);
        }
        let recovered = VerifyingKey::recover_from_prehash(&self.signing_hash(), &parsed, recovery)
            .map_err(|_| InvalidEvmKey)?;
        let address =
            evm_address(recovered.to_encoded_point(false).as_bytes()).map_err(|_| InvalidEvmKey)?;
        if &address != expected_address {
            return Err(InvalidEvmKey);
        }
        let mut payload = self.fields();
        rlp_integer(&[recovery.to_byte()], &mut payload);
        rlp_integer(&signature[..32], &mut payload);
        rlp_integer(&signature[32..], &mut payload);
        let mut raw = vec![0x02];
        raw.extend_from_slice(&rlp_list(&payload));
        Ok(raw)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    type SignedCall = (Eip1559Call, [u8; 64], u8, [u8; 20]);

    fn signed_call() -> Result<SignedCall, Box<dyn std::error::Error>> {
        let call = Eip1559Call {
            chain_id: 1,
            nonce: 7,
            max_priority_fee_per_gas: 2,
            max_fee_per_gas: 3,
            gas_limit: 30_000,
            to: [0x11; 20],
            data: vec![0xde, 0xad, 0xbe, 0xef],
        };
        let key = SigningKey::from_slice(&[1; 32]).map_err(|_| "invalid scalar")?;
        let (signature, recovery) = key
            .sign_prehash_recoverable(&call.signing_hash())
            .map_err(|_| "signature failed")?;
        let address = evm_address(key.verifying_key().to_encoded_point(false).as_bytes())
            .map_err(|_| "invalid public key")?;
        Ok((
            call,
            signature.to_bytes().into(),
            recovery.to_byte(),
            address,
        ))
    }

    #[test]
    fn assemble_matches_local_signing() -> Result<(), Box<dyn std::error::Error>> {
        let (call, signature, recovery, address) = signed_call()?;
        let assembled = call.assemble(&signature, recovery, &address)?;
        assert_eq!(assembled, call.sign(&[1; 32])?);
        assert_eq!(assembled[0], 2);
        assert_eq!(
            address,
            [
                0x1a, 0x64, 0x2f, 0x0e, 0x3c, 0x3a, 0xf5, 0x45, 0xe7, 0xac, 0xbd, 0x38, 0xb0, 0x72,
                0x51, 0xb3, 0x99, 0x09, 0x14, 0xf1,
            ]
        );
        Ok(())
    }

    #[test]
    fn assemble_refuses_malformed_scalars() -> Result<(), Box<dyn std::error::Error>> {
        let (call, _, recovery, address) = signed_call()?;
        for signature in [[0; 64], [0xff; 64]] {
            assert_eq!(
                call.assemble(&signature, recovery, &address),
                Err(InvalidEvmKey)
            );
        }
        Ok(())
    }

    #[test]
    fn assemble_refuses_high_s() -> Result<(), Box<dyn std::error::Error>> {
        let (call, mut signature, recovery, address) = signed_call()?;
        signature[32..].copy_from_slice(&[
            0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff,
            0xff, 0xfe, 0xba, 0xae, 0xdc, 0xe6, 0xaf, 0x48, 0xa0, 0x3b, 0xbf, 0xd2, 0x5e, 0x8c,
            0xd0, 0x36, 0x41, 0x40,
        ]);
        assert_eq!(
            call.assemble(&signature, recovery, &address),
            Err(InvalidEvmKey)
        );
        Ok(())
    }

    #[test]
    fn assemble_refuses_non_evm_recovery_ids() -> Result<(), Box<dyn std::error::Error>> {
        let (call, signature, _, address) = signed_call()?;
        for recovery in 2..=u8::MAX {
            assert_eq!(
                call.assemble(&signature, recovery, &address),
                Err(InvalidEvmKey)
            );
        }
        Ok(())
    }

    #[test]
    fn assemble_refuses_wrong_sender_and_changed_call() -> Result<(), Box<dyn std::error::Error>> {
        let (mut call, signature, recovery, mut address) = signed_call()?;
        assert_eq!(
            call.assemble(&signature, recovery ^ 1, &address),
            Err(InvalidEvmKey)
        );
        address[0] ^= 1;
        assert_eq!(
            call.assemble(&signature, recovery, &address),
            Err(InvalidEvmKey)
        );
        address[0] ^= 1;
        call.nonce += 1;
        assert_eq!(
            call.assemble(&signature, recovery, &address),
            Err(InvalidEvmKey)
        );
        Ok(())
    }
}
