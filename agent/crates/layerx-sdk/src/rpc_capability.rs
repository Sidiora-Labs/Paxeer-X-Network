//! `capability.*` over the version 1 Agent operation envelope.

pub use agent::{CapabilityAuthority, CapabilityRecord, CapabilityResponse, CapabilityState};

mod agent {
    use layerx_agent_api::capability::{
        AmountCeiling, CapabilityAttenuate, CapabilityCreate, CapabilityDimensions, CapabilityList,
        CapabilityRevoke, ExplicitSet, RateCeiling,
    };
    use layerx_agent_api::error::{Key, RequestId};
    use layerx_agent_api::identity::{ActivityType, Asset, ContractError, Counterparty, Purpose};
    use layerx_agent_api::{Amount, TimestampSeconds};
    use serde_json::{json, Value};

    use crate::agent_envelope::{AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError};
    use crate::rpc_history::lower_hex;
    use crate::rpc_subscription::{decimal, object, text, violation};
    use crate::Operation;

    /// Authority the owner actually used for one capability response.
    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct CapabilityAuthority {
        pub tenant: String,
        pub agent_did: String,
        pub authority_ref: String,
        pub protocol_authority: Vec<u8>,
    }

    #[derive(Clone, Copy, Debug, Eq, PartialEq)]
    pub enum CapabilityState {
        Active,
        Revoked,
        Expired,
    }

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct CapabilityRecord {
        pub capability_id: String,
        pub parent_id: Option<String>,
        pub tenant: String,
        pub agent_did: String,
        pub dimensions: CapabilityDimensions,
        pub state: CapabilityState,
        pub created_at_ms: u64,
        pub created_at_sequence: u64,
        pub revoked_at_ms: Option<u64>,
        pub revoked_at_sequence: Option<u64>,
    }

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct CapabilityResponse<T> {
        pub authority: CapabilityAuthority,
        pub value: T,
    }

    fn strs(values: &[impl AsRef<str>]) -> Value {
        Value::Array(values.iter().map(|item| Value::String(item.as_ref().into())).collect())
    }

    fn dimensions_value(dimensions: &CapabilityDimensions) -> Value {
        json!({
            "activity_types": dimensions.activity_types.values().iter().map(|item| item.0.to_string()).collect::<Vec<_>>(),
            "counterparties": strs(&dimensions.counterparties.values().iter().map(Counterparty::as_str).collect::<Vec<_>>()),
            "assets": strs(&dimensions.assets.values().iter().map(Asset::as_str).collect::<Vec<_>>()),
            "amount_ceilings": dimensions.amount_ceilings.values().iter().map(|ceiling| json!({
                "asset": ceiling.asset.as_str(),
                "amount": ceiling.amount.0.to_string(),
            })).collect::<Vec<_>>(),
            "rate_ceilings": dimensions.rate_ceilings.values().iter().map(|ceiling| json!({
                "window_seconds": ceiling.window_seconds.0.to_string(),
                "maximum_actions": ceiling.maximum_actions.to_string(),
            })).collect::<Vec<_>>(),
            "purpose_constraints": strs(&dimensions.purpose_constraints.values().iter().map(Purpose::as_str).collect::<Vec<_>>()),
            "expiry": dimensions.expiry.0.to_string(),
        })
    }

    fn hex32(value: &Value, operation: Operation) -> Result<String, EnvelopeError> {
        value
            .as_str()
            .filter(|text| text.len() == 64 && lower_hex(text).is_some())
            .map(str::to_owned)
            .ok_or_else(|| violation(operation))
    }

    fn optional<T>(
        value: &Value,
        operation: Operation,
        decode: fn(&Value, Operation) -> Result<T, EnvelopeError>,
    ) -> Result<Option<T>, EnvelopeError> {
        match value {
            Value::Null => Ok(None),
            value => decode(value, operation).map(Some),
        }
    }

    fn amount(value: &Value, operation: Operation) -> Result<u128, EnvelopeError> {
        value
            .as_str()
            .and_then(|text| {
                let amount: u128 = text.parse().ok()?;
                (amount.to_string() == text).then_some(amount)
            })
            .ok_or_else(|| violation(operation))
    }

    fn array<'a>(value: &'a Value, operation: Operation) -> Result<&'a Vec<Value>, EnvelopeError> {
        value.as_array().ok_or_else(|| violation(operation))
    }

    fn texts<T>(
        value: &Value,
        operation: Operation,
        new: fn(String) -> Result<T, ContractError>,
    ) -> Result<ExplicitSet<T>, EnvelopeError> {
        Ok(ExplicitSet::allow(
            array(value, operation)?
                .iter()
                .map(|item| new(text(item, operation)?).map_err(|_| violation(operation)))
                .collect::<Result<_, _>>()?,
        ))
    }

    fn decode_dimensions(
        value: &Value,
        operation: Operation,
    ) -> Result<CapabilityDimensions, EnvelopeError> {
        let dimensions = object(
            value,
            &[
                "activity_types",
                "counterparties",
                "assets",
                "amount_ceilings",
                "rate_ceilings",
                "purpose_constraints",
                "expiry",
            ],
            operation,
        )?;
        let activity_types = array(&dimensions["activity_types"], operation)?
            .iter()
            .map(|item| {
                u16::try_from(decimal(item, operation)?)
                    .map(ActivityType)
                    .map_err(|_| violation(operation))
            })
            .collect::<Result<_, _>>()?;
        let amount_ceilings = array(&dimensions["amount_ceilings"], operation)?
            .iter()
            .map(|item| {
                let ceiling = object(item, &["asset", "amount"], operation)?;
                Ok(AmountCeiling {
                    asset: Asset::new(text(&ceiling["asset"], operation)?)
                        .map_err(|_| violation(operation))?,
                    amount: Amount(amount(&ceiling["amount"], operation)?),
                })
            })
            .collect::<Result<_, EnvelopeError>>()?;
        let rate_ceilings = array(&dimensions["rate_ceilings"], operation)?
            .iter()
            .map(|item| {
                let ceiling = object(item, &["window_seconds", "maximum_actions"], operation)?;
                Ok(RateCeiling {
                    window_seconds: TimestampSeconds(decimal(&ceiling["window_seconds"], operation)?),
                    maximum_actions: decimal(&ceiling["maximum_actions"], operation)?,
                })
            })
            .collect::<Result<_, EnvelopeError>>()?;
        CapabilityDimensions {
            activity_types: ExplicitSet::allow(activity_types),
            counterparties: texts(&dimensions["counterparties"], operation, Counterparty::new)?,
            assets: texts(&dimensions["assets"], operation, Asset::new)?,
            amount_ceilings: ExplicitSet::allow(amount_ceilings),
            rate_ceilings: ExplicitSet::allow(rate_ceilings),
            purpose_constraints: texts(&dimensions["purpose_constraints"], operation, Purpose::new)?,
            expiry: TimestampSeconds(decimal(&dimensions["expiry"], operation)?),
        }
        .validate()
        .map_err(|_| violation(operation))
    }

    fn decode_record(value: &Value, operation: Operation) -> Result<CapabilityRecord, EnvelopeError> {
        let record = object(
            value,
            &[
                "capability_id",
                "parent_id",
                "tenant",
                "agent_did",
                "dimensions",
                "state",
                "created_at_ms",
                "created_at_sequence",
                "revoked_at_ms",
                "revoked_at_sequence",
            ],
            operation,
        )?;
        let state = match record["state"].as_str() {
            Some("active") => CapabilityState::Active,
            Some("revoked") => CapabilityState::Revoked,
            Some("expired") => CapabilityState::Expired,
            _ => return Err(violation(operation)),
        };
        let revoked_at_ms = optional(&record["revoked_at_ms"], operation, decimal)?;
        let revoked_at_sequence = optional(&record["revoked_at_sequence"], operation, decimal)?;
        if revoked_at_ms.is_some() != revoked_at_sequence.is_some()
            || (state == CapabilityState::Revoked) != revoked_at_ms.is_some()
        {
            return Err(violation(operation));
        }
        Ok(CapabilityRecord {
            capability_id: hex32(&record["capability_id"], operation)?,
            parent_id: optional(&record["parent_id"], operation, hex32)?,
            tenant: text(&record["tenant"], operation)?,
            agent_did: text(&record["agent_did"], operation)?,
            dimensions: decode_dimensions(&record["dimensions"], operation)?,
            state,
            created_at_ms: decimal(&record["created_at_ms"], operation)?,
            created_at_sequence: decimal(&record["created_at_sequence"], operation)?,
            revoked_at_ms,
            revoked_at_sequence,
        })
    }

    fn decode_authority(
        value: &Value,
        operation: Operation,
        tenant: &str,
        agent_did: &str,
    ) -> Result<CapabilityAuthority, EnvelopeError> {
        let authority = object(
            value,
            &["tenant", "agent_did", "authority_ref", "protocol_authority"],
            operation,
        )?;
        let decoded = CapabilityAuthority {
            tenant: text(&authority["tenant"], operation)?,
            agent_did: text(&authority["agent_did"], operation)?,
            authority_ref: text(&authority["authority_ref"], operation)?,
            protocol_authority: authority["protocol_authority"]
                .as_str()
                .and_then(lower_hex)
                .filter(|bytes| !bytes.is_empty())
                .ok_or_else(|| violation(operation))?,
        };
        if decoded.tenant != tenant || decoded.agent_did != agent_did {
            return Err(violation(operation));
        }
        Ok(decoded)
    }

    fn owned(record: &CapabilityRecord, tenant: &str, agent_did: &str) -> bool {
        record.tenant == tenant && record.agent_did == agent_did
    }

    impl AgentEnvelopeTransport {
        fn capability_record(
            &self,
            operation: Operation,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            request: &Value,
            coordinates: (&str, &str),
        ) -> Result<CapabilityResponse<CapabilityRecord>, EnvelopeError> {
            let success =
                self.send_operation(operation, request_id, request, Some(credential), Some(key))?;
            let response = object(&success.value, &["authority", "value"], operation)?;
            let (tenant, agent_did) = coordinates;
            let authority = decode_authority(&response["authority"], operation, tenant, agent_did)?;
            let value = decode_record(&response["value"], operation)?;
            if !owned(&value, tenant, agent_did) {
                return Err(violation(operation));
            }
            Ok(CapabilityResponse { authority, value })
        }

        /// Creates one root capability for the authenticated owner.
        ///
        /// # Errors
        ///
        /// Returns the established error envelope, or `Unknown` when the outcome cannot be
        /// established; reconcile with the same idempotency key.
        pub fn capability_create(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            request: &CapabilityCreate,
        ) -> Result<CapabilityResponse<CapabilityRecord>, EnvelopeError> {
            self.capability_record(
                Operation::CapabilityCreate,
                request_id,
                key,
                credential,
                &json!({
                    "tenant": request.tenant.as_str(),
                    "agent_did": request.agent_did.as_str(),
                    "dimensions": dimensions_value(&request.dimensions),
                }),
                (request.tenant.as_str(), request.agent_did.as_str()),
            )
        }

        /// Derives one capability no wider than its parent.
        ///
        /// # Errors
        ///
        /// See [`Self::capability_create`].
        pub fn capability_attenuate(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            request: &CapabilityAttenuate,
        ) -> Result<CapabilityResponse<CapabilityRecord>, EnvelopeError> {
            let response = self.capability_record(
                Operation::CapabilityAttenuate,
                request_id,
                key,
                credential,
                &json!({
                    "tenant": request.tenant.as_str(),
                    "agent_did": request.agent_did.as_str(),
                    "parent_id": request.parent_id.as_str(),
                    "dimensions": dimensions_value(&request.dimensions),
                }),
                (request.tenant.as_str(), request.agent_did.as_str()),
            )?;
            if response.value.parent_id.as_deref() != Some(request.parent_id.as_str()) {
                return Err(violation(Operation::CapabilityAttenuate));
            }
            Ok(response)
        }

        /// Lists the capabilities of the authenticated owner.
        ///
        /// # Errors
        ///
        /// Returns the established error envelope, `Transport` or `Decode`.
        pub fn capability_list(
            &self,
            request_id: RequestId,
            credential: &EnvelopeCredential,
            request: &CapabilityList,
        ) -> Result<CapabilityResponse<Vec<CapabilityRecord>>, EnvelopeError> {
            let operation = Operation::CapabilityList;
            let (tenant, agent_did) = (request.tenant.as_str(), request.agent_did.as_str());
            let success = self.send_operation(
                operation,
                request_id,
                &json!({"tenant": tenant, "agent_did": agent_did}),
                Some(credential),
                None,
            )?;
            let response = object(&success.value, &["authority", "value"], operation)?;
            let authority = decode_authority(&response["authority"], operation, tenant, agent_did)?;
            let value = object(&response["value"], &["capabilities"], operation)?;
            let records = array(&value["capabilities"], operation)?
                .iter()
                .map(|record| decode_record(record, operation))
                .collect::<Result<Vec<_>, _>>()?;
            if records.iter().any(|record| !owned(record, tenant, agent_did))
                || records
                    .windows(2)
                    .any(|pair| pair[0].capability_id >= pair[1].capability_id)
            {
                return Err(violation(operation));
            }
            Ok(CapabilityResponse {
                authority,
                value: records,
            })
        }

        /// Revokes one capability and every capability derived from it.
        ///
        /// # Errors
        ///
        /// See [`Self::capability_create`].
        pub fn capability_revoke(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            request: &CapabilityRevoke,
        ) -> Result<CapabilityResponse<CapabilityRecord>, EnvelopeError> {
            let response = self.capability_record(
                Operation::CapabilityRevoke,
                request_id,
                key,
                credential,
                &json!({
                    "tenant": request.tenant.as_str(),
                    "agent_did": request.agent_did.as_str(),
                    "capability_id": request.capability_id.as_str(),
                }),
                (request.tenant.as_str(), request.agent_did.as_str()),
            )?;
            if response.value.capability_id != request.capability_id.as_str()
                || response.value.state != CapabilityState::Revoked
            {
                return Err(violation(Operation::CapabilityRevoke));
            }
            Ok(response)
        }
    }
}
