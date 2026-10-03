use std::io::{self, Read, Write};
use std::net::{TcpStream, ToSocketAddrs};
use std::sync::Arc;
use std::time::Duration;

use layerx_types::clock::{Clock, Deadline};

use serde_json::{json, Value};
use tungstenite::{
    client::IntoClientRequest, protocol::WebSocketConfig, stream::MaybeTlsStream, Message,
    WebSocket,
};

use crate::programs::LayerXKeyCredential;
use crate::rpc::{encode_hex, RpcError};

const SUBSCRIBE_ID: &str = "1";
const UNSUBSCRIBE_ID: &str = "2";
const MAX_PENDING_EVENTS: usize = 16;

#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum SubscriptionTopic {
    Receipts,
    Checkpoints,
    Account,
}

pub struct RpcSubscription {
    socket: WebSocket<MaybeTlsStream<DeadlineStream>>,
    id: String,
    cursor: Option<u64>,
}

impl RpcSubscription {
    #[must_use]
    pub fn id(&self) -> &str {
        &self.id
    }

    /// Feed position last delivered on this subscription, or the position it resumed from.
    /// Present it to `RpcClient::subscribe_from` after a lost connection to resume the topic.
    #[must_use]
    pub fn cursor(&self) -> Option<u64> {
        self.cursor
    }

    /// Returns an unverified notification, or None after an idle polling interval.
    /// Reconcile notifications through reads and verified commitment waits.
    /// # Errors
    /// Refuses binary frames, malformed events, mismatched subscriptions and lost connections.
    pub fn next_event(&mut self) -> Result<Option<Value>, RpcError> {
        let Some(value) = receive_json(&mut self.socket)? else {
            return Ok(None);
        };
        let (result, cursor) = notification(&value, &self.id)?;
        self.cursor = Some(cursor);
        Ok(Some(result))
    }

    /// Cancels this subscription on the server and confirms the acknowledgement. Notifications
    /// already in flight are discarded after advancing the cursor past them.
    /// # Errors
    /// Reports refused cancellations, malformed acknowledgements and lost connections.
    pub fn unsubscribe(&mut self) -> Result<(), RpcError> {
        transport(&mut self.socket)?.reset()?;
        self.socket
            .send(Message::Text(
                json!({"jsonrpc":"2.0", "id":UNSUBSCRIBE_ID, "method":"lx_unsubscribe", "params":[self.id.as_str()]})
                    .to_string()
                    .into(),
            ))
            .map_err(|_| RpcError::Transport)?;
        for _ in 0..=MAX_PENDING_EVENTS {
            let value = receive_until(&mut self.socket)?.ok_or(RpcError::Transport)?;
            if value.get("method").and_then(Value::as_str) == Some("lx_subscription") {
                let (_, cursor) = notification(&value, &self.id)?;
                self.cursor = Some(cursor);
                continue;
            }
            return cancellation(&value);
        }
        Err(RpcError::Transport)
    }

    /// # Errors
    /// Reports a failed connection close.
    pub fn close(&mut self) -> Result<(), RpcError> {
        transport(&mut self.socket)?.reset()?;
        self.socket.close(None).map_err(|_| RpcError::Transport)
    }
}

fn parameters(
    topic: SubscriptionTopic,
    account: Option<[u8; 32]>,
    cursor: Option<u64>,
) -> Result<Value, RpcError> {
    let mut params = match (topic, account) {
        (SubscriptionTopic::Receipts, None) => json!(["receipts"]),
        (SubscriptionTopic::Checkpoints, None) => json!(["checkpoints"]),
        (SubscriptionTopic::Account, Some(account)) => json!(["account", encode_hex(&account)]),
        _ => return Err(RpcError::InvalidRequest),
    };
    if let Some(cursor) = cursor {
        params
            .as_array_mut()
            .ok_or(RpcError::InvalidRequest)?
            .push(json!(cursor.to_string()));
    }
    Ok(params)
}

pub(crate) fn connect(
    endpoint: &url::Url,
    credential: Option<&LayerXKeyCredential>,
    tls: Option<std::sync::Arc<rustls::ClientConfig>>,
    clock: Arc<dyn Clock>,
    topic: SubscriptionTopic,
    account: Option<[u8; 32]>,
    cursor: Option<u64>,
) -> Result<RpcSubscription, RpcError> {
    let params = parameters(topic, account, cursor)?;
    let mut endpoint = endpoint.clone();
    endpoint.set_path(&format!("{}/ws", endpoint.path().trim_end_matches('/')));
    let secure = endpoint.scheme() == "https";
    endpoint
        .set_scheme(if secure { "wss" } else { "ws" })
        .map_err(|()| RpcError::InvalidRequest)?;
    let host = endpoint
        .host_str()
        .ok_or(RpcError::InvalidRequest)?
        .trim_start_matches('[')
        .trim_end_matches(']');
    let port = endpoint
        .port_or_known_default()
        .ok_or(RpcError::InvalidRequest)?;
    let mut deadline =
        Deadline::start(clock.as_ref(), Duration::from_secs(30)).map_err(RpcError::Clock)?;
    let mut stream = None;
    for address in resolve(
        host,
        port,
        deadline
            .remaining(clock.as_ref())
            .map_err(RpcError::Clock)?,
    )? {
        let remaining = deadline
            .remaining(clock.as_ref())
            .map_err(RpcError::Clock)?;
        if remaining.is_zero() {
            return Err(RpcError::Transport);
        }
        if let Ok(socket) = TcpStream::connect_timeout(&address, remaining) {
            stream = Some(socket);
            break;
        }
    }
    let stream = DeadlineStream {
        socket: stream.ok_or(RpcError::Transport)?,
        clock,
        deadline,
    };
    let mut request = endpoint
        .as_str()
        .into_client_request()
        .map_err(|_| RpcError::InvalidRequest)?;
    if let Some(credential) = credential {
        let value = credential
            .authorization()
            .map_err(RpcError::Configuration)?;
        request.headers_mut().insert(
            "Authorization",
            value.parse().map_err(|_| RpcError::InvalidRequest)?,
        );
    }
    let config = WebSocketConfig::default()
        .max_message_size(Some(9 * 1_048_576))
        .max_frame_size(Some(9 * 1_048_576))
        .max_write_buffer_size(262_144);
    let (mut socket, _) = tungstenite::client_tls_with_config(
        request,
        stream,
        Some(config),
        tls.map(tungstenite::Connector::Rustls),
    )
    .map_err(|_| RpcError::Transport)?;
    socket
        .send(Message::Text(
            json!({"jsonrpc":"2.0", "id":SUBSCRIBE_ID, "method":"lx_subscribe", "params":params})
                .to_string()
                .into(),
        ))
        .map_err(|_| RpcError::Transport)?;
    let response = receive_until(&mut socket)?.ok_or(RpcError::Transport)?;
    let id = acknowledgement(&response)?;
    Ok(RpcSubscription { socket, id, cursor })
}

fn receive_json(
    socket: &mut WebSocket<MaybeTlsStream<DeadlineStream>>,
) -> Result<Option<Value>, RpcError> {
    transport(socket)?.reset()?;
    receive_until(socket)
}

fn receive_until(
    socket: &mut WebSocket<MaybeTlsStream<DeadlineStream>>,
) -> Result<Option<Value>, RpcError> {
    while !transport(socket)?.remaining()?.is_zero() {
        match socket.read() {
            Ok(Message::Text(text)) => {
                return serde_json::from_str(&text)
                    .map(Some)
                    .map_err(|_| RpcError::InvalidResponse)
            }
            Ok(Message::Ping(_) | Message::Pong(_)) => {
                socket.flush().map_err(|_| RpcError::Transport)?;
            }
            Err(tungstenite::Error::Io(error))
                if matches!(
                    error.kind(),
                    std::io::ErrorKind::WouldBlock | std::io::ErrorKind::TimedOut
                ) =>
            {
                return Ok(None)
            }
            Ok(Message::Close(_)) | Err(_) => return Err(RpcError::Transport),
            Ok(_) => return Err(RpcError::InvalidResponse),
        }
    }
    Ok(None)
}

struct DeadlineStream {
    socket: TcpStream,
    clock: Arc<dyn Clock>,
    deadline: Deadline,
}

impl DeadlineStream {
    fn reset(&mut self) -> Result<(), RpcError> {
        self.deadline = Deadline::start(self.clock.as_ref(), Duration::from_secs(30))
            .map_err(RpcError::Clock)?;
        Ok(())
    }

    fn remaining(&mut self) -> Result<Duration, RpcError> {
        self.deadline
            .remaining(self.clock.as_ref())
            .map_err(RpcError::Clock)
    }

    fn budget(&mut self) -> io::Result<Duration> {
        let remaining = self
            .remaining()
            .map_err(|_| io::Error::other("clock unavailable"))?;
        if remaining.is_zero() {
            return Err(io::Error::new(
                io::ErrorKind::TimedOut,
                "subscription deadline reached",
            ));
        }
        Ok(remaining)
    }
}

impl Read for DeadlineStream {
    fn read(&mut self, bytes: &mut [u8]) -> io::Result<usize> {
        let budget = self.budget()?;
        self.socket.set_read_timeout(Some(budget))?;
        let read = self.socket.read(bytes)?;
        self.budget()?;
        Ok(read)
    }
}

impl Write for DeadlineStream {
    fn write(&mut self, bytes: &[u8]) -> io::Result<usize> {
        let budget = self.budget()?;
        self.socket.set_write_timeout(Some(budget))?;
        let written = self.socket.write(bytes)?;
        self.budget()?;
        Ok(written)
    }

    fn flush(&mut self) -> io::Result<()> {
        self.budget()?;
        self.socket.flush()
    }
}

fn transport(
    socket: &mut WebSocket<MaybeTlsStream<DeadlineStream>>,
) -> Result<&mut DeadlineStream, RpcError> {
    match socket.get_mut() {
        MaybeTlsStream::Plain(stream) => Ok(stream),
        MaybeTlsStream::Rustls(stream) => Ok(&mut stream.sock),
        _ => Err(RpcError::Transport),
    }
}

fn resolve(host: &str, port: u16, budget: Duration) -> Result<Vec<std::net::SocketAddr>, RpcError> {
    use std::sync::atomic::{AtomicUsize, Ordering};
    static ACTIVE: AtomicUsize = AtomicUsize::new(0);
    struct Permit;
    impl Drop for Permit {
        fn drop(&mut self) {
            ACTIVE.fetch_sub(1, Ordering::AcqRel);
        }
    }
    ACTIVE
        .fetch_update(Ordering::AcqRel, Ordering::Acquire, |active| {
            (active < 16).then_some(active + 1)
        })
        .map_err(|_| RpcError::Transport)?;
    let permit = Permit;
    let host = host.to_owned();
    let (sender, receiver) = std::sync::mpsc::sync_channel(1);
    std::thread::Builder::new()
        .name("rpc-subscription-resolver".into())
        .spawn(move || {
            let _permit = permit;
            let result = (host.as_str(), port)
                .to_socket_addrs()
                .map(|addresses| addresses.take(16).collect());
            let _ = sender.send(result);
        })
        .map_err(|_| RpcError::Transport)?;
    receiver
        .recv_timeout(budget)
        .map_err(|_| RpcError::Transport)?
        .map_err(|_| RpcError::Transport)
}

fn remote(error: &Value) -> RpcError {
    let Some(code) = error["code"].as_i64() else {
        return RpcError::InvalidResponse;
    };
    let Some(message) = error["message"].as_str() else {
        return RpcError::InvalidResponse;
    };
    RpcError::Remote {
        code,
        message: message.to_owned(),
        data: error.get("data").cloned(),
    }
}

fn acknowledgement(value: &Value) -> Result<String, RpcError> {
    if value.as_object().is_none_or(|object| object.len() != 3)
        || value["jsonrpc"] != "2.0"
        || value["id"] != SUBSCRIBE_ID
    {
        return Err(RpcError::InvalidResponse);
    }
    if let Some(error) = value.get("error") {
        return Err(remote(error));
    }
    let id = value["result"]
        .as_str()
        .filter(|id| !id.is_empty() && id.len() <= 256)
        .ok_or(RpcError::InvalidResponse)?;
    Ok(id.to_owned())
}

fn cancellation(value: &Value) -> Result<(), RpcError> {
    if value.as_object().is_none_or(|object| object.len() != 3)
        || value["jsonrpc"] != "2.0"
        || value["id"] != UNSUBSCRIBE_ID
    {
        return Err(RpcError::InvalidResponse);
    }
    if let Some(error) = value.get("error") {
        return Err(remote(error));
    }
    if value["result"] != Value::Bool(true) {
        return Err(RpcError::InvalidResponse);
    }
    Ok(())
}

fn cursor_value(text: &str) -> Option<u64> {
    let value: u64 = text.parse().ok()?;
    (value.to_string() == text).then_some(value)
}

fn notification(value: &Value, id: &str) -> Result<(Value, u64), RpcError> {
    if value.as_object().is_none_or(|object| object.len() != 3)
        || value["jsonrpc"] != "2.0"
        || value["method"] != "lx_subscription"
        || value["params"]
            .as_object()
            .is_none_or(|object| object.len() != 3)
        || value["params"]["subscription"] != id
        || !value["params"]["result"].is_object()
    {
        return Err(RpcError::InvalidResponse);
    }
    let cursor = value["params"]["cursor"]
        .as_str()
        .and_then(cursor_value)
        .ok_or(RpcError::InvalidResponse)?;
    Ok((value["params"]["result"].clone(), cursor))
}

pub use agent::{
    GapNotice, SubscriptionFilter, SubscriptionHealth, SubscriptionRecord, SubscriptionScope,
    TenantValue,
};

mod agent {
    use layerx_agent_api::error::RequestId;
    use layerx_agent_api::idempotency::Key;
    use serde_json::{json, Map, Value};

    use crate::agent_envelope::{
        canonical_u64, AgentEnvelopeTransport, EnvelopeCredential, EnvelopeError,
    };
    use crate::Operation;

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct SubscriptionScope {
        pub tenant: String,
        pub agent: String,
        pub capability: String,
    }

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct TenantValue {
        pub tenant: String,
        pub value: String,
    }

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct SubscriptionFilter {
        pub agents: Vec<TenantValue>,
        pub accounts: Vec<TenantValue>,
        pub activity_types: Vec<u16>,
        pub modules: Vec<TenantValue>,
        pub assets: Vec<TenantValue>,
        pub counterparties: Vec<TenantValue>,
        pub result_classes: Vec<i32>,
    }

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct SubscriptionRecord {
        pub subscription_id: String,
        pub scope: SubscriptionScope,
        pub filter: SubscriptionFilter,
        pub start: u64,
        pub last_acknowledged: u64,
        pub delivery_target: String,
        pub paused: bool,
    }

    #[derive(Clone, Copy, Debug, Eq, PartialEq)]
    pub struct GapNotice {
        pub missing_first: u64,
        pub missing_last: u64,
        pub backfill_cursor: u64,
        pub backfill_attempted: bool,
    }

    #[derive(Clone, Debug, Eq, PartialEq)]
    pub struct SubscriptionHealth {
        pub scope: SubscriptionScope,
        pub subscription_id: String,
        pub last_acknowledged: u64,
        pub last_delivery_at: Option<u64>,
        pub pending_backfill: Option<GapNotice>,
    }

    pub(crate) fn object<'a>(
        value: &'a Value,
        fields: &[&str],
        operation: Operation,
    ) -> Result<&'a Map<String, Value>, EnvelopeError> {
        value
            .as_object()
            .filter(|object| {
                object.len() == fields.len()
                    && fields.iter().all(|field| object.contains_key(*field))
            })
            .ok_or_else(|| violation(operation))
    }

    pub(crate) fn violation(operation: Operation) -> EnvelopeError {
        if operation.mutating() {
            EnvelopeError::Unknown { operation }
        } else {
            EnvelopeError::Decode { operation }
        }
    }

    pub(crate) fn text(value: &Value, operation: Operation) -> Result<String, EnvelopeError> {
        value
            .as_str()
            .filter(|text| !text.is_empty())
            .map(str::to_owned)
            .ok_or_else(|| violation(operation))
    }

    pub(crate) fn decimal(value: &Value, operation: Operation) -> Result<u64, EnvelopeError> {
        value
            .as_str()
            .and_then(canonical_u64)
            .ok_or_else(|| violation(operation))
    }

    fn scope_value(scope: &SubscriptionScope) -> Value {
        json!({"tenant": scope.tenant, "agent": scope.agent, "capability": scope.capability})
    }

    fn tenant_values(values: &[TenantValue]) -> Value {
        Value::Array(
            values
                .iter()
                .map(|item| json!({"tenant": item.tenant, "value": item.value}))
                .collect(),
        )
    }

    fn filter_value(filter: &SubscriptionFilter) -> Value {
        json!({
            "agents": tenant_values(&filter.agents),
            "accounts": tenant_values(&filter.accounts),
            "activity_types": filter.activity_types.iter().map(u16::to_string).collect::<Vec<_>>(),
            "modules": tenant_values(&filter.modules),
            "assets": tenant_values(&filter.assets),
            "counterparties": tenant_values(&filter.counterparties),
            "result_classes": filter.result_classes,
        })
    }

    fn target_value(scope: &SubscriptionScope, subscription_id: &str) -> Value {
        json!({"scope": scope_value(scope), "subscription_id": subscription_id})
    }

    fn decode_scope(
        value: &Value,
        operation: Operation,
    ) -> Result<SubscriptionScope, EnvelopeError> {
        let scope = object(value, &["tenant", "agent", "capability"], operation)?;
        Ok(SubscriptionScope {
            tenant: text(&scope["tenant"], operation)?,
            agent: text(&scope["agent"], operation)?,
            capability: text(&scope["capability"], operation)?,
        })
    }

    fn decode_tenant_values(
        value: &Value,
        operation: Operation,
    ) -> Result<Vec<TenantValue>, EnvelopeError> {
        value
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|item| {
                let item = object(item, &["tenant", "value"], operation)?;
                Ok(TenantValue {
                    tenant: text(&item["tenant"], operation)?,
                    value: text(&item["value"], operation)?,
                })
            })
            .collect()
    }

    fn decode_filter(
        value: &Value,
        operation: Operation,
    ) -> Result<SubscriptionFilter, EnvelopeError> {
        let filter = object(
            value,
            &[
                "agents",
                "accounts",
                "activity_types",
                "modules",
                "assets",
                "counterparties",
                "result_classes",
            ],
            operation,
        )?;
        let activity_types = filter["activity_types"]
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|item| u16::try_from(decimal(item, operation)?).map_err(|_| violation(operation)))
            .collect::<Result<_, _>>()?;
        let result_classes = filter["result_classes"]
            .as_array()
            .ok_or_else(|| violation(operation))?
            .iter()
            .map(|item| {
                item.as_i64()
                    .and_then(|number| i32::try_from(number).ok())
                    .ok_or_else(|| violation(operation))
            })
            .collect::<Result<_, _>>()?;
        Ok(SubscriptionFilter {
            agents: decode_tenant_values(&filter["agents"], operation)?,
            accounts: decode_tenant_values(&filter["accounts"], operation)?,
            activity_types,
            modules: decode_tenant_values(&filter["modules"], operation)?,
            assets: decode_tenant_values(&filter["assets"], operation)?,
            counterparties: decode_tenant_values(&filter["counterparties"], operation)?,
            result_classes,
        })
    }

    fn decode_record(
        value: &Value,
        operation: Operation,
    ) -> Result<SubscriptionRecord, EnvelopeError> {
        let record = object(
            value,
            &[
                "subscription_id",
                "scope",
                "filter",
                "start",
                "last_acknowledged",
                "delivery_target",
                "paused",
            ],
            operation,
        )?;
        Ok(SubscriptionRecord {
            subscription_id: text(&record["subscription_id"], operation)?,
            scope: decode_scope(&record["scope"], operation)?,
            filter: decode_filter(&record["filter"], operation)?,
            start: decimal(&record["start"], operation)?,
            last_acknowledged: decimal(&record["last_acknowledged"], operation)?,
            delivery_target: text(&record["delivery_target"], operation)?,
            paused: record["paused"]
                .as_bool()
                .ok_or_else(|| violation(operation))?,
        })
    }

    fn decode_gap(value: &Value, operation: Operation) -> Result<GapNotice, EnvelopeError> {
        let gap = object(
            value,
            &[
                "missing_first",
                "missing_last",
                "backfill_cursor",
                "backfill_attempted",
            ],
            operation,
        )?;
        let notice = GapNotice {
            missing_first: decimal(&gap["missing_first"], operation)?,
            missing_last: decimal(&gap["missing_last"], operation)?,
            backfill_cursor: decimal(&gap["backfill_cursor"], operation)?,
            backfill_attempted: gap["backfill_attempted"]
                .as_bool()
                .ok_or_else(|| violation(operation))?,
        };
        if notice.missing_first > notice.missing_last {
            return Err(violation(operation));
        }
        Ok(notice)
    }

    fn decode_health(
        value: &Value,
        operation: Operation,
    ) -> Result<SubscriptionHealth, EnvelopeError> {
        let health = object(
            value,
            &[
                "target",
                "last_acknowledged",
                "last_delivery_at",
                "pending_backfill",
            ],
            operation,
        )?;
        let target = object(&health["target"], &["scope", "subscription_id"], operation)?;
        Ok(SubscriptionHealth {
            scope: decode_scope(&target["scope"], operation)?,
            subscription_id: text(&target["subscription_id"], operation)?,
            last_acknowledged: decimal(&health["last_acknowledged"], operation)?,
            last_delivery_at: match &health["last_delivery_at"] {
                Value::Null => None,
                value => Some(decimal(value, operation)?),
            },
            pending_backfill: match &health["pending_backfill"] {
                Value::Null => None,
                value => Some(decode_gap(value, operation)?),
            },
        })
    }

    impl AgentEnvelopeTransport {
        fn subscription_record(
            &self,
            operation: Operation,
            request_id: RequestId,
            key: Key,
            request: &Value,
            credential: &EnvelopeCredential,
        ) -> Result<SubscriptionRecord, EnvelopeError> {
            let success =
                self.send_operation(operation, request_id, request, Some(credential), Some(key))?;
            decode_record(&success.value, operation)
        }

        /// Creates one durable subscription bound to the authenticated owner scope.
        ///
        /// # Errors
        ///
        /// Returns the established error envelope, or `Unknown` when the outcome cannot be
        /// established; reconcile with the same idempotency key.
        #[allow(clippy::too_many_arguments)]
        pub fn subscription_create(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
            filter: &SubscriptionFilter,
            start: u64,
            delivery_target: &str,
        ) -> Result<SubscriptionRecord, EnvelopeError> {
            self.subscription_record(
                Operation::SubscriptionCreate,
                request_id,
                key,
                &json!({
                    "scope": scope_value(scope),
                    "filter": filter_value(filter),
                    "start": start.to_string(),
                    "delivery_target": delivery_target,
                }),
                credential,
            )
        }

        /// Lists the subscriptions visible to the authenticated owner scope.
        ///
        /// # Errors
        ///
        /// Returns the established error envelope, `Transport` or `Decode`.
        pub fn subscription_list(
            &self,
            request_id: RequestId,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
        ) -> Result<Vec<SubscriptionRecord>, EnvelopeError> {
            let operation = Operation::SubscriptionList;
            let success = self.send_operation(
                operation,
                request_id,
                &json!({"scope": scope_value(scope)}),
                Some(credential),
                None,
            )?;
            success
                .value
                .as_array()
                .ok_or_else(|| violation(operation))?
                .iter()
                .map(|record| decode_record(record, operation))
                .collect()
        }

        /// Pauses one subscription.
        ///
        /// # Errors
        ///
        /// See [`Self::subscription_create`].
        pub fn subscription_pause(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
            subscription_id: &str,
        ) -> Result<SubscriptionRecord, EnvelopeError> {
            self.subscription_record(
                Operation::SubscriptionPause,
                request_id,
                key,
                &target_value(scope, subscription_id),
                credential,
            )
        }

        /// Resumes one subscription.
        ///
        /// # Errors
        ///
        /// See [`Self::subscription_create`].
        pub fn subscription_resume(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
            subscription_id: &str,
        ) -> Result<SubscriptionRecord, EnvelopeError> {
            self.subscription_record(
                Operation::SubscriptionResume,
                request_id,
                key,
                &target_value(scope, subscription_id),
                credential,
            )
        }

        /// Deletes one subscription; success is the daemon's JSON null after durable deletion.
        ///
        /// # Errors
        ///
        /// See [`Self::subscription_create`].
        pub fn subscription_delete(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
            subscription_id: &str,
        ) -> Result<(), EnvelopeError> {
            let operation = Operation::SubscriptionDelete;
            let success = self.send_operation(
                operation,
                request_id,
                &target_value(scope, subscription_id),
                Some(credential),
                Some(key),
            )?;
            if success.value.is_null() {
                Ok(())
            } else {
                Err(violation(operation))
            }
        }

        /// Reports delivery health for one subscription.
        ///
        /// # Errors
        ///
        /// Returns the established error envelope, `Transport` or `Decode`.
        pub fn subscription_health(
            &self,
            request_id: RequestId,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
            subscription_id: &str,
        ) -> Result<SubscriptionHealth, EnvelopeError> {
            let operation = Operation::SubscriptionHealth;
            let success = self.send_operation(
                operation,
                request_id,
                &target_value(scope, subscription_id),
                Some(credential),
                None,
            )?;
            decode_health(&success.value, operation)
        }

        /// Acknowledges delivery through `cursor`.
        ///
        /// # Errors
        ///
        /// See [`Self::subscription_create`].
        pub fn subscription_acknowledge(
            &self,
            request_id: RequestId,
            key: Key,
            credential: &EnvelopeCredential,
            scope: &SubscriptionScope,
            subscription_id: &str,
            cursor: u64,
        ) -> Result<SubscriptionRecord, EnvelopeError> {
            self.subscription_record(
                Operation::SubscriptionAcknowledge,
                request_id,
                key,
                &json!({
                    "scope": scope_value(scope),
                    "subscription_id": subscription_id,
                    "cursor": cursor.to_string(),
                }),
                credential,
            )
        }
    }
}

pub(crate) use agent::{decimal, object, text, violation};

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn subscriptions_bind_selectors_acknowledgements_and_notifications() {
        assert!(parameters(SubscriptionTopic::Account, None, None).is_err());
        assert!(parameters(SubscriptionTopic::Receipts, Some([1; 32]), None).is_err());
        assert_eq!(
            parameters(SubscriptionTopic::Checkpoints, None, None).ok(),
            Some(json!(["checkpoints"]))
        );
        assert_eq!(
            acknowledgement(&json!({"jsonrpc":"2.0", "id":"1", "result":"sub"}))
                .ok()
                .as_deref(),
            Some("sub")
        );
        assert!(acknowledgement(&json!({"jsonrpc":"2.0", "id":"2", "result":"sub"})).is_err());
        assert!(acknowledgement(
            &json!({"jsonrpc":"2.0", "id":"1", "result":{"state":"accepted"}})
        )
        .is_err());
        let event = json!({"jsonrpc":"2.0", "method":"lx_subscription", "params":{"subscription":"sub", "result":{"state":"pending"}, "cursor":"41"}});
        assert_eq!(
            notification(&event, "sub").ok(),
            Some((json!({"state":"pending"}), 41))
        );
        assert!(notification(&event, "another-subscription").is_err());
    }

    #[test]
    fn resume_selectors_carry_a_canonical_cursor_after_the_topic() {
        assert_eq!(
            parameters(SubscriptionTopic::Receipts, None, Some(41)).ok(),
            Some(json!(["receipts", "41"]))
        );
        assert_eq!(
            parameters(SubscriptionTopic::Checkpoints, None, Some(0)).ok(),
            Some(json!(["checkpoints", "0"]))
        );
        assert_eq!(
            parameters(SubscriptionTopic::Account, Some([0xab; 32]), Some(7)).ok(),
            Some(json!(["account", "ab".repeat(32), "7"]))
        );
        assert_eq!(cursor_value("41"), Some(41));
        assert_eq!(cursor_value("0"), Some(0));
        for text in ["041", "", "-1", "+1", "18446744073709551616", " 1"] {
            assert_eq!(cursor_value(text), None);
        }
    }

    #[test]
    fn notifications_without_a_canonical_cursor_are_refused() {
        for params in [
            json!({"subscription":"sub", "result":{"state":"pending"}}),
            json!({"subscription":"sub", "result":{"state":"pending"}, "cursor":41}),
            json!({"subscription":"sub", "result":{"state":"pending"}, "cursor":"041"}),
            json!({"subscription":"sub", "result":{"state":"pending"}, "cursor":""}),
            json!({"subscription":"sub", "result":"pending", "cursor":"41"}),
        ] {
            assert!(notification(
                &json!({"jsonrpc":"2.0", "method":"lx_subscription", "params":params}),
                "sub"
            )
            .is_err());
        }
    }

    #[test]
    fn cancellations_require_an_affirmative_acknowledgement() {
        assert!(cancellation(&json!({"jsonrpc":"2.0", "id":"2", "result":true})).is_ok());
        for value in [
            json!({"jsonrpc":"2.0", "id":"1", "result":true}),
            json!({"jsonrpc":"2.0", "id":"2", "result":false}),
            json!({"jsonrpc":"2.0", "id":"2", "result":"true"}),
            json!({"jsonrpc":"2.0", "id":"2"}),
        ] {
            assert!(cancellation(&value).is_err());
        }
        assert!(matches!(
            cancellation(
                &json!({"jsonrpc":"2.0", "id":"2", "error":{"code":-32602, "message":"Unknown subscription"}})
            ),
            Err(RpcError::Remote {
                code: -32602,
                ref message,
                data: None
            }) if message == "Unknown subscription"
        ));
    }
}
