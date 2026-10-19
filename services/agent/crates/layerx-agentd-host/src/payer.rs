use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use layerx_agent_api::error::RequestId;
use layerx_agent_api::idempotency::Key;
use layerx_agent_api::identity::{
    AgentDid, CapabilityId, NativeActivity, NativeLocalGrantConsentV1, NativeSendPrepareRequestV1,
    NativeSendPurposeV1, SignedNativeSendPurposeV1,
};
use layerx_crypto::payments::Grant;
use layerx_crypto::send::EnvelopeOptions;
use layerx_human_kms::attestor::{
    new_session_id, AttestorError, AttestorSigner, SendApproval, SignedSend,
};
use layerx_mcp::tools::web::{ExactTerms, GrantTerms, WebPayer, WebPayerError};
use layerx_proof::receipt::{verify, verify_sequencer_signature, AuthorizedBatch};
use layerx_sdk::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
use layerx_types::clock::{Clock, ClockReading};
use layerx_types::payload::ModuleRegistry;
use serde_json::{json, Value};
use sha2::{Digest, Sha256};
use zeroize::Zeroizing;

pub use layerx_agentd::human_runtime::McpNativeWebContextV1 as NativePaymentContext;

pub struct RegisteredOwnerWebAccess {
    pub local_grant: NativeLocalGrantConsentV1,
    pub commitment: [u8; 32],
    pub fee_limit: u128,
    pub send_validity_ms: u64,
}

pub type NativeContextSource =
    Box<dyn FnMut(&ExactTerms) -> Result<NativePaymentContext, WebPayerError> + Send>;
pub type NativePurposeSource = Box<
    dyn FnMut(
            &ExactTerms,
            &NativePaymentContext,
            &SignedSend,
        ) -> Result<NativeSendPurposeV1, WebPayerError>
        + Send,
>;

pub struct ProductionWebPayer {
    payer_did: String,
    principal: String,
    signer: Arc<AttestorSigner>,
    assertion_source: PathBuf,
    transport: AgentEnvelopeTransport,
    credential: EnvelopeCredential,
    registry: ModuleRegistry,
    sequencer_public_key: [u8; 32],
    clock: Arc<dyn Clock>,
    last_clock: Option<ClockReading>,
    grant_validity_seconds: u64,
    native_context: NativeContextSource,
    native_purpose: NativePurposeSource,
    request_id: u64,
    owner_check: Box<dyn FnMut() -> Result<(), WebPayerError> + Send>,
    search_quote: Box<dyn FnMut([u8; 32]) -> Result<u128, WebPayerError> + Send>,
}

impl ProductionWebPayer {
    #[allow(clippy::too_many_arguments)]
    pub fn from_owner<A: layerx_agentd::human_runtime::HumanAuthorityBoundary + Send + 'static>(
        owner: layerx_agentd::human_runtime::SharedAgentOwner<A>,
        credential: layerx_agentd::session::SessionCredential,
        payer_did: String,
        principal: String,
        signer: Arc<AttestorSigner>,
        assertion_source: PathBuf,
        transport: AgentEnvelopeTransport,
        sequencer_public_key: [u8; 32],
        clock: Arc<dyn Clock>,
        grant_validity_seconds: u64,
        access: RegisteredOwnerWebAccess,
    ) -> Result<Self, WebPayerError> {
        let public_key = signer.public_key();
        let registry = owner
            .mcp_native_web_registry(&credential, public_key)
            .map_err(human_error)?;
        if owner
            .mcp_native_web_sequencer_key(&credential, public_key)
            .map_err(human_error)?
            != sequencer_public_key
        {
            return Err(WebPayerError::Refused);
        }
        let sdk_credential = EnvelopeCredential::new(
            credential.tenant().as_str(),
            credential.session_id().0,
            credential.token_id(),
            credential.generation(),
        )
        .map_err(envelope_error)?;
        let context_owner = owner.clone();
        let context_credential = credential.clone();
        let context_source = Box::new(move |terms: &ExactTerms| {
            context_owner
                .mcp_native_web_context(
                    &context_credential,
                    public_key,
                    terms.recipient,
                    terms.asset,
                    terms.amount,
                    terms.idempotency_key,
                    access.local_grant.clone(),
                    access.commitment,
                    access.fee_limit,
                    access.send_validity_ms,
                )
                .map_err(human_error)
        });
        let purpose_owner = owner.clone();
        let purpose_credential = credential.clone();
        let purpose_registry = registry.clone();
        let purpose_source = Box::new(
            move |_: &ExactTerms, context: &NativePaymentContext, signed: &SignedSend| {
                let activity =
                    layerx_wire::activity::decode_unsigned(signed.canonical(), &purpose_registry)
                        .map_err(|_| WebPayerError::Refused)?;
                let prepare = layerx_agentd::human::HumanPrepare {
                    activity_type: activity.activity_type().value(),
                    actor: context.actor.clone(),
                    authority: std::str::from_utf8(activity.authority())
                        .map_err(|_| WebPayerError::Refused)?
                        .to_owned(),
                    account_sequence: activity.account_sequence(),
                    not_before: activity.timestamp_bound().not_before,
                    not_after: activity.timestamp_bound().not_after,
                    idempotency_key: hex(&activity.idempotency_key()),
                    fee_limit: activity.fee_limit(),
                    payload: activity.payload().to_vec(),
                    payload_hash: activity.payload_hash(),
                    capability_id: Some(
                        context
                            .capability_id
                            .to_bytes()
                            .map_err(|_| WebPayerError::Refused)?,
                    ),
                };
                purpose_owner
                    .mcp_native_web_purpose(
                        purpose_credential.clone(),
                        public_key,
                        prepare,
                        context.clone(),
                        signed.canonical(),
                    )
                    .map_err(human_error)
            },
        );
        let quote_owner = owner.clone();
        let quote_credential = credential.clone();
        let search_quote = Box::new(move |asset| {
            quote_owner
                .mcp_native_web_search_quote(&quote_credential, public_key, asset)
                .map_err(human_error)
        });
        let owner_check = Box::new(move || {
            owner
                .mcp_native_web_registry(&credential, public_key)
                .map(|_| ())
                .map_err(human_error)
        });
        Self::new(
            payer_did,
            principal,
            signer,
            assertion_source,
            transport,
            sdk_credential,
            registry,
            sequencer_public_key,
            clock,
            grant_validity_seconds,
            context_source,
            purpose_source,
            owner_check,
            search_quote,
        )
    }

    #[allow(clippy::too_many_arguments)]
    fn new(
        payer_did: String,
        principal: String,
        signer: Arc<AttestorSigner>,
        assertion_source: PathBuf,
        transport: AgentEnvelopeTransport,
        credential: EnvelopeCredential,
        registry: ModuleRegistry,
        sequencer_public_key: [u8; 32],
        clock: Arc<dyn Clock>,
        grant_validity_seconds: u64,
        native_context: NativeContextSource,
        native_purpose: NativePurposeSource,
        owner_check: Box<dyn FnMut() -> Result<(), WebPayerError> + Send>,
        search_quote: Box<dyn FnMut([u8; 32]) -> Result<u128, WebPayerError> + Send>,
    ) -> Result<Self, WebPayerError> {
        let owner_did = format!("did:layerx:{}", hex(&signer.public_key()));
        if payer_did != owner_did
            || principal.is_empty()
            || principal.len() > 1024
            || principal.chars().any(char::is_control)
            || sequencer_public_key == [0; 32]
            || grant_validity_seconds == 0
            || grant_validity_seconds > 3600
        {
            return Err(WebPayerError::Refused);
        }
        let payer = Self {
            payer_did,
            principal,
            signer,
            assertion_source,
            transport,
            credential,
            registry,
            sequencer_public_key,
            clock,
            last_clock: None,
            grant_validity_seconds,
            native_context,
            native_purpose,
            request_id: 0,
            owner_check,
            search_quote,
        };
        let _assertion = payer.assertion()?;
        Ok(payer)
    }

    fn assertion(&self) -> Result<Zeroizing<String>, WebPayerError> {
        let bytes = Zeroizing::new(
            layerx_agentd::config::read_protected_source(&self.assertion_source, 8192)
                .map_err(|_| WebPayerError::Unavailable)?,
        );
        let text = std::str::from_utf8(&bytes)
            .map_err(|_| WebPayerError::Refused)?
            .trim();
        if text.is_empty() || text.bytes().any(|byte| !byte.is_ascii_graphic()) {
            return Err(WebPayerError::Refused);
        }
        Ok(Zeroizing::new(text.to_owned()))
    }

    fn now(&mut self) -> Result<ClockReading, WebPayerError> {
        let now = self
            .clock
            .sample(Duration::from_secs(1))
            .map_err(|_| WebPayerError::Unavailable)?;
        if now.generation == [0; 16] {
            return Err(WebPayerError::Unavailable);
        }
        if let Some(previous) = self.last_clock {
            now.follows(previous)
                .map_err(|_| WebPayerError::Unavailable)?;
        }
        self.last_clock = Some(now);
        Ok(now)
    }

    fn next_request(&mut self) -> Result<RequestId, WebPayerError> {
        self.request_id = self
            .request_id
            .checked_add(1)
            .ok_or(WebPayerError::Unavailable)?;
        Ok(RequestId(self.request_id))
    }

    fn receipt(
        &self,
        value: &Value,
        terms: &ExactTerms,
        source: [u8; 32],
        expected_activity_id: [u8; 32],
    ) -> Result<Option<Vec<u8>>, WebPayerError> {
        let state = value
            .pointer("/submission/state")
            .and_then(Value::as_str)
            .ok_or(WebPayerError::Unavailable)?;
        if matches!(state, "Failed" | "Expired") {
            return Err(WebPayerError::Refused);
        }
        if state != "Executed" {
            if !value.get("receipt").is_some_and(Value::is_null) {
                return Err(WebPayerError::Refused);
            }
            return Ok(None);
        }
        let receipt = value.get("receipt").ok_or(WebPayerError::Refused)?;
        let canonical = receipt
            .get("canonical_bytes")
            .and_then(Value::as_str)
            .and_then(unhex)
            .ok_or(WebPayerError::Refused)?;
        let decoded = verify_sequencer_signature(&canonical, self.sequencer_public_key)
            .map_err(|_| WebPayerError::Refused)?;
        let protocol = decoded.protocol().ok_or(WebPayerError::Refused)?;
        let activity_id = value
            .get("activity_id")
            .and_then(Value::as_str)
            .and_then(unhex32)
            .ok_or(WebPayerError::Refused)?;
        if activity_id != expected_activity_id
            || protocol.activity_id() != activity_id
            || protocol.module_id() != 1
            || protocol.operation() != 5
            || protocol.result_code() != 0
            || protocol.from() != source
            || protocol.to() != terms.recipient
            || protocol.asset() != terms.asset
            || protocol.amount() != terms.amount
        {
            return Err(WebPayerError::Refused);
        }
        let batch = receipt
            .get("authorised_batch")
            .ok_or(WebPayerError::Refused)?;
        let field = |name: &str| {
            batch
                .get(name)
                .and_then(Value::as_str)
                .and_then(unhex32)
                .ok_or(WebPayerError::Refused)
        };
        if field("sequencer_public_key")? != self.sequencer_public_key {
            return Err(WebPayerError::Refused);
        }
        let authority = AuthorizedBatch::new(
            field("batch_id")?,
            field("asset")?,
            field("previous_state_root")?,
            field("resulting_state_root")?,
            self.sequencer_public_key,
        );
        verify(&canonical, &authority).map_err(|_| WebPayerError::Refused)?;
        Ok(Some(canonical))
    }
}

impl WebPayer for ProductionWebPayer {
    fn validate_search_quote(
        &mut self,
        asset: [u8; 32],
        amount: u128,
    ) -> Result<(), WebPayerError> {
        (self.owner_check)()?;
        if amount == 0 || (self.search_quote)(asset)? != amount {
            return Err(WebPayerError::Refused);
        }
        Ok(())
    }

    fn grant(&mut self, terms: &GrantTerms) -> Result<Vec<u8>, WebPayerError> {
        (self.owner_check)()?;
        if terms.payer_did != self.payer_did || terms.idempotency_key == [0; 32] {
            return Err(WebPayerError::Refused);
        }
        let lifetime_ms = self
            .grant_validity_seconds
            .checked_mul(1000)
            .ok_or(WebPayerError::Unavailable)?;
        let expiration = self
            .now()?
            .unix_milliseconds
            .checked_add(lifetime_ms)
            .ok_or(WebPayerError::Unavailable)?;
        let grant = Grant {
            id: [0; 32],
            from: terms.payer_account,
            recipient: terms.recipient,
            asset: terms.asset,
            per_draw_maximum: terms.amount,
            allowance: terms.amount,
            recurring: false,
            window_length: 0,
            expiration,
            purpose_hash: terms.purpose_hash,
            has_reference: false,
            reference_hash: [0; 32],
            revocation_sequence: 0,
            public_key: self.signer.public_key(),
            signature: [0; 64],
        };
        self.signer
            .sign_payment_grant(&grant, &hex(&terms.idempotency_key), &self.assertion()?)
            .map_err(attestor_error)
    }

    fn pay(&mut self, terms: &ExactTerms) -> Result<Vec<u8>, WebPayerError> {
        (self.owner_check)()?;
        if terms.payer_did != self.payer_did
            || terms.idempotency_key == [0; 32]
            || terms.amount == 0
        {
            return Err(WebPayerError::Refused);
        }
        let now = self.now()?;
        let context = (self.native_context)(terms)?;
        let debit = &context.debit;
        if context.actor != self.payer_did
            || debit.from != terms.payer_account
            || debit.to != terms.recipient
            || debit.asset != terms.asset
            || debit.amount != terms.amount
            || debit.idempotency_key != terms.idempotency_key
            || debit.authorization_kind != 1
            || !debit.conditions.is_empty()
            || now.unix_milliseconds < context.not_before
            || now.unix_milliseconds >= context.not_after
            || now.unix_milliseconds >= context.purpose_expires_at_ms
            || context.local_grant.owner_public_key != self.signer.public_key()
        {
            return Err(WebPayerError::Refused);
        }
        let authorization_session = new_session_id("mcp-web-debit").map_err(attestor_error)?;
        let activity_session = new_session_id("mcp-web-activity").map_err(attestor_error)?;
        let options = EnvelopeOptions {
            actor: &context.actor,
            public_key: self.signer.public_key(),
            protocol_version: debit.protocol_version,
            network_id: debit.network_id,
            identity_sequence: context.identity_sequence,
            idempotency_key: terms.idempotency_key,
            fee_limit: context.fee_limit,
            not_before: context.not_before,
            not_after: context.not_after,
        };
        let approval_expiry_seconds = context
            .not_after
            .checked_div(1000)
            .ok_or(WebPayerError::Refused)?;
        if approval_expiry_seconds <= now.unix_seconds() {
            return Err(WebPayerError::Refused);
        }
        let signed = self
            .signer
            .sign_native_send(
                debit,
                &options,
                &SendApproval {
                    principal: &self.principal,
                    authorization_session: &authorization_session,
                    activity_session: &activity_session,
                    expires_at: approval_expiry_seconds,
                },
                || {
                    self.assertion()
                        .map(|value| value.to_string())
                        .map_err(|_| AttestorError::Configuration("owner assertion unavailable"))
                },
            )
            .map_err(attestor_error)?;
        let envelope = layerx_crypto::send::encode_send_envelope(signed.payload(), &options)
            .map_err(|_| WebPayerError::Refused)?;
        if envelope.canonical != signed.canonical() {
            return Err(WebPayerError::Refused);
        }
        let signature = layerx_types::activity::Signature::new(signed.activity().signature())
            .map_err(|_| WebPayerError::Refused)?;
        let submitted_bytes = layerx_wire::activity::encode_signed_envelope(
            &envelope.envelope.attach_signature(signature),
        )
        .map_err(|_| WebPayerError::Refused)?;
        let submitted_activity =
            layerx_wire::activity::decode_signed(&submitted_bytes, &self.registry)
                .map_err(|_| WebPayerError::Refused)?;
        let expected_activity_id = layerx_wire::hash::activity_id(&submitted_activity)
            .map_err(|_| WebPayerError::Refused)?;
        let purpose = (self.native_purpose)(terms, &context, &signed)?;
        if purpose.tenant.as_str() != self.credential.tenant()
            || purpose.session_id.to_bytes().ok() != Some(self.credential.session_id())
            || purpose.generation != self.credential.generation()
            || purpose.owner_public_key != self.signer.public_key()
            || purpose.capability_id != context.capability_id
            || purpose.idempotency_key != terms.idempotency_key
            || purpose.commitment != context.commitment
            || purpose.economic_action != context.economic_action
            || purpose.expires_at_ms != context.purpose_expires_at_ms
            || purpose.canonical_digest != <[u8; 32]>::from(Sha256::digest(signed.canonical()))
        {
            return Err(WebPayerError::Refused);
        }
        let purpose_session = new_session_id("mcp-web-purpose").map_err(attestor_error)?;
        let consent = self
            .signer
            .sign_native_send_purpose(&purpose, &purpose_session, &self.assertion()?)
            .map_err(attestor_error)?;
        let activity = layerx_wire::activity::decode_unsigned(signed.canonical(), &self.registry)
            .map_err(|_| WebPayerError::Refused)?;
        if activity.payload() != signed.payload() {
            return Err(WebPayerError::Refused);
        }
        let authority = std::str::from_utf8(activity.authority())
            .map_err(|_| WebPayerError::Refused)?
            .to_owned();
        let request = NativeSendPrepareRequestV1 {
            activity: NativeActivity::new(1, 5).map_err(|_| WebPayerError::Refused)?,
            actor: AgentDid::new(context.actor.clone()).map_err(|_| WebPayerError::Refused)?,
            authority,
            account_sequence: context.identity_sequence,
            not_before: context.not_before,
            not_after: context.not_after,
            idempotency_key: terms.idempotency_key,
            fee_limit: context.fee_limit,
            payload: signed.payload().to_vec(),
            payload_hash: activity.payload_hash(),
            capability_id: context.capability_id.clone(),
            purpose: SignedNativeSendPurposeV1 {
                purpose,
                owner_public_key: self.signer.public_key(),
                signature: *consent.signature(),
            },
            local_grant: Some(context.local_grant.clone()),
        };
        let key = Key::new(terms.idempotency_key).map_err(|_| WebPayerError::Refused)?;
        let request_id = self.next_request()?;
        let prepared = self
            .transport
            .prepare_native_send_mcp(request_id, key, &request, &self.credential, &self.registry)
            .map_err(envelope_error)?
            .value;
        if prepared.canonical_bytes != signed.canonical()
            || prepared.signing_preimage != *signed.activity().digest()
            || prepared.approval_required
        {
            return Err(WebPayerError::Refused);
        }
        let request_id = self.next_request()?;
        let submit = layerx_agent_api::submit::SubmitRequest {
            preparation_ref: layerx_agent_api::submit::PreparationRef::new(hex(
                &prepared.preparation_id
            ))
            .map_err(|_| WebPayerError::Refused)?,
            signature: layerx_agent_api::submit::SignatureBytes::new(
                signed.activity().signature().to_vec(),
            )
            .map_err(|_| WebPayerError::Refused)?,
            approval_release_ref: None,
        };
        let submit_body = layerx_sdk::native_effect::encode_native_send_submit(
            &submit,
            &self.signer.public_key(),
        )
        .map_err(envelope_error)?;
        let submitted = self
            .transport
            .mcp_invoke(
                request_id,
                &self.credential,
                "activity.submit",
                &submit_body,
                key,
            )
            .map_err(envelope_error)?;
        if let Some(receipt) =
            self.receipt(&submitted.value, terms, debit.from, expected_activity_id)?
        {
            return Ok(receipt);
        }
        let submission_ref = submitted
            .value
            .pointer("/submission/submission_ref")
            .and_then(Value::as_str)
            .ok_or(WebPayerError::Unavailable)?
            .to_owned();
        let request_id = self.next_request()?;
        let mut read_key = [0_u8; 32];
        getrandom::fill(&mut read_key).map_err(|_| WebPayerError::Unavailable)?;
        let read_key = Key::new(read_key).map_err(|_| WebPayerError::Unavailable)?;
        let tracked = self
            .transport
            .mcp_invoke(
                request_id,
                &self.credential,
                "activity.track",
                &json!({"submission_ref":submission_ref}),
                read_key,
            )
            .map_err(envelope_error)?;
        self.receipt(&tracked.value, terms, debit.from, expected_activity_id)?
            .ok_or(WebPayerError::Unavailable)
    }
}

fn attestor_error(error: AttestorError) -> WebPayerError {
    match error {
        AttestorError::Timeout { .. } | AttestorError::Unavailable { .. } => {
            WebPayerError::Unavailable
        }
        _ => WebPayerError::Refused,
    }
}
fn envelope_error(error: EnvelopeError) -> WebPayerError {
    match error {
        EnvelopeError::Refused(_)
        | EnvelopeError::InvalidCredential
        | EnvelopeError::InvalidRequest => WebPayerError::Refused,
        _ => WebPayerError::Unavailable,
    }
}
fn hex(bytes: &[u8]) -> String {
    use std::fmt::Write as _;
    bytes
        .iter()
        .fold(String::with_capacity(bytes.len() * 2), |mut text, byte| {
            let _ = write!(text, "{byte:02x}");
            text
        })
}
fn unhex(text: &str) -> Option<Vec<u8>> {
    if text.len() % 2 != 0
        || !text
            .bytes()
            .all(|byte| matches!(byte,b'0'..=b'9'|b'a'..=b'f'))
    {
        return None;
    }
    text.as_bytes()
        .chunks_exact(2)
        .map(|pair| u8::from_str_radix(std::str::from_utf8(pair).ok()?, 16).ok())
        .collect()
}
fn unhex32(text: &str) -> Option<[u8; 32]> {
    unhex(text)?.try_into().ok()
}

fn human_error(error: layerx_agentd::human::HumanOperationError) -> WebPayerError {
    match error {
        layerx_agentd::human::HumanOperationError::Unavailable => WebPayerError::Unavailable,
        _ => WebPayerError::Refused,
    }
}
