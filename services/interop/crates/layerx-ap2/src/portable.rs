use layerx_interop_gateway::adapter::{AdapterDescriptor, ConformanceSuite};
use layerx_portable::{ExternalEvidenceKind, ExternalEvidenceVerifier};
use serde::{Deserialize, Serialize};

use crate::{
    ap2_adapter_descriptor, Ap2Error, KeyResolver, MandateVerifier, VerificationContext,
    VerifiedMandates,
};

/// `LayerX` transport envelope for the exact AP2 Checkout and Payment Mandate
/// presentations that must be verified as one bound pair.
pub const AP2_MANDATE_PAIR_MEDIA_TYPE: &str = "application/vnd.layerx.ap2-mandate-pair+json";

/// Borrowed AP2 presentation pair for constructing the adapter input without
/// changing either exact SD-JWT presentation.
#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Ap2MandatePair<'a> {
    checkout_mandate: &'a str,
    payment_mandate: &'a str,
}

impl<'a> Ap2MandatePair<'a> {
    #[must_use]
    pub const fn new(checkout_mandate: &'a str, payment_mandate: &'a str) -> Self {
        Self {
            checkout_mandate,
            payment_mandate,
        }
    }

    /// Encodes the pair without decoding or re-encoding either AP2 token.
    ///
    /// # Errors
    ///
    /// Refuses empty presentations and serialization failures.
    pub fn to_json(&self) -> Result<Vec<u8>, Ap2Error> {
        if self.checkout_mandate.is_empty() || self.payment_mandate.is_empty() {
            return Err(Ap2Error::Malformed("portable mandate presentation"));
        }
        serde_json::to_vec(self).map_err(|_| Ap2Error::Malformed("portable mandate presentation"))
    }
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
struct OwnedMandatePair {
    checkout_mandate: String,
    payment_mandate: String,
}

/// AP2 mandate verifier wired into the portable external-evidence boundary.
/// The caller supplies the real conformance-suite declaration and trust-store
/// resolver; no key, time, nonce, audience or usage evidence is ambient.
pub struct Ap2ExternalMandateVerifier<'a, R> {
    descriptor: AdapterDescriptor,
    verifier: MandateVerifier<'a, R>,
}

impl<'a, R: KeyResolver> Ap2ExternalMandateVerifier<'a, R> {
    /// Constructs the verifier against the exact AP2 spec pin and a real
    /// caller-supplied conformance suite.
    ///
    /// # Errors
    ///
    /// Refuses an invalid adapter or conformance declaration.
    pub fn new(resolver: &'a R, conformance: ConformanceSuite) -> Result<Self, Ap2Error> {
        Ok(Self {
            descriptor: ap2_adapter_descriptor(conformance)?,
            verifier: MandateVerifier::new(resolver),
        })
    }
}

impl<'context, R: KeyResolver> ExternalEvidenceVerifier<VerificationContext<'context>>
    for Ap2ExternalMandateVerifier<'_, R>
{
    type Verified = VerifiedMandates;
    type Error = Ap2Error;

    fn descriptor(&self) -> &AdapterDescriptor {
        &self.descriptor
    }

    fn evidence_kind(&self) -> ExternalEvidenceKind {
        ExternalEvidenceKind::Mandate
    }

    fn media_type(&self) -> &str {
        AP2_MANDATE_PAIR_MEDIA_TYPE
    }

    fn verify(
        &self,
        payload: &[u8],
        context: &VerificationContext<'context>,
    ) -> Result<Self::Verified, Self::Error> {
        let pair: OwnedMandatePair = serde_json::from_slice(payload)
            .map_err(|_| Ap2Error::Malformed("portable mandate presentation"))?;
        if pair.checkout_mandate.is_empty() || pair.payment_mandate.is_empty() {
            return Err(Ap2Error::Malformed("portable mandate presentation"));
        }
        self.verifier
            .verify(&pair.checkout_mandate, &pair.payment_mandate, context)
    }
}
