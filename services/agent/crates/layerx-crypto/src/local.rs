//! Zeroizing in-process Ed25519 DID signer with an optional EVM transaction key.

use std::fmt;

use ed25519_dalek::{Signer as _, SigningKey};

use crate::account_derivation::DerivedAccount;
use crate::evm_transaction::Eip1559Call;
use crate::redact::Secret;
use crate::signer::{
    AgentSignature, DidSignature, SignError, SignFuture, Signer, SigningRequest,
    TransactionSignFuture,
};

/// In-process signer whose secrets are zeroized on drop and never rendered.
pub struct LocalSigner {
    seed: Secret<[u8; 32]>,
    public_key: [u8; 32],
    evm: Option<(Secret<[u8; 32]>, [u8; 20])>,
}

impl LocalSigner {
    /// Imports a private Ed25519 seed into zeroizing memory.
    #[must_use]
    pub fn new(seed: [u8; 32]) -> Self {
        Self::from_secret(Secret::new(seed))
    }

    /// Imports an already protected private Ed25519 seed.
    #[must_use]
    pub fn from_secret(seed: Secret<[u8; 32]>) -> Self {
        let public_key =
            seed.expose(|value| SigningKey::from_bytes(value).verifying_key().to_bytes());
        Self {
            seed,
            public_key,
            evm: None,
        }
    }

    /// Imports both keys of a derived account; the EVM key is absent when an
    /// external wallet holds it.
    #[must_use]
    pub fn from_derived_account(account: &DerivedAccount) -> Self {
        let mut signer = Self::new(*account.layerx_seed());
        signer.evm = account
            .evm_secret()
            .map(|secret| (Secret::new(*secret), account.evm_address()));
        signer
    }
}

impl fmt::Debug for LocalSigner {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        formatter.write_str("LocalSigner([redacted key])")
    }
}

impl Signer for LocalSigner {
    fn public_key(&self) -> [u8; 32] {
        self.public_key
    }

    fn sign<'a>(&'a self, request: SigningRequest<'a>) -> SignFuture<'a> {
        Box::pin(async move {
            let signing_key = self.seed.expose(SigningKey::from_bytes);
            let signature = signing_key.sign(&request.message().digest());
            Ok(DidSignature::new(signature.to_bytes()))
        })
    }

    fn evm_address(&self) -> Option<[u8; 20]> {
        self.evm.as_ref().map(|(_, address)| *address)
    }

    fn sign_transaction<'a>(&'a self, call: &'a Eip1559Call) -> TransactionSignFuture<'a> {
        Box::pin(async move {
            let Some((secret, _)) = &self.evm else {
                return Err(SignError::EvmKeyUnavailable);
            };
            let (signature, recovery) = secret.expose(|value| {
                k256::ecdsa::SigningKey::from_slice(value)
                    .and_then(|key| key.sign_prehash_recoverable(&call.signing_hash()))
                    .map_err(|_| SignError::KeyRejected)
            })?;
            let mut bytes = [0_u8; 65];
            bytes[..64].copy_from_slice(&signature.to_bytes());
            bytes[64] = 27 + recovery.to_byte();
            AgentSignature::new(bytes).map_err(|_| SignError::KeyRejected)
        })
    }
}
