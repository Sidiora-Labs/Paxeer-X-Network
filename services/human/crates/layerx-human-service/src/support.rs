//! Principal-scoped durable support conversations.

use std::fmt::{Display, Formatter};

use serde::{Deserialize, Serialize};
use sha2::{Digest as _, Sha256};

use crate::store::{PrincipalScope, RowKey, StoreError, Table};
use crate::trace::TraceId;

const ROW_PREFIX: &str = "support_conversation_";
const ID_DOMAIN: &[u8] = b"LayerX support conversation v1\0";
const MESSAGE_DOMAIN: &[u8] = b"LayerX support message v1\0";
const MAX_BODY_CHARS: usize = 2_000;
const MAX_MESSAGES: usize = 200;
const MAX_CONVERSATIONS: usize = 100;
const MAX_IDEMPOTENCY_KEY_BYTES: usize = 200;

/// Shell in which the conversation began.
#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Shell {
    /// Compact mobile screen.
    Mobile,
    /// Docked desktop panel.
    Desktop,
}

/// Optional first-message topic.
#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Topic {
    /// Adding money.
    Deposit,
    /// Taking money out.
    Withdrawal,
    /// Managed agents.
    Agents,
    /// Account and sign-in.
    Account,
    /// A consented error report.
    Report,
}

/// The persisted author of a support message.
#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum Author {
    /// Authenticated person.
    You,
    /// Authenticated support operator.
    Support,
}

/// Current conversation ownership.
#[derive(Clone, Copy, Debug, Deserialize, Eq, PartialEq, Serialize)]
#[serde(rename_all = "kebab-case")]
pub enum ConversationState {
    /// The person's last message awaits support.
    WaitingForSupport,
    /// A support reply awaits the person.
    WaitingForYou,
    /// Support closed the conversation.
    Resolved,
}

/// One persisted message whose author is never inferred by the browser.
#[allow(clippy::struct_field_names)]
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Message {
    message_id: String,
    author: Author,
    body: String,
    sent_at: u64,
    read: bool,
    topic: Option<Topic>,
}

impl Message {
    /// Stable message identifier.
    #[must_use]
    pub fn message_id(&self) -> &str {
        &self.message_id
    }

    /// Persisted author.
    #[must_use]
    pub const fn author(&self) -> Author {
        self.author
    }

    /// Validated body.
    #[must_use]
    pub fn body(&self) -> &str {
        &self.body
    }

    /// Caller-injected creation timestamp.
    #[must_use]
    pub const fn sent_at(&self) -> u64 {
        self.sent_at
    }

    /// Whether the authenticated person read the message.
    #[must_use]
    pub const fn is_read(&self) -> bool {
        self.read
    }

    /// First-message topic, when declared.
    #[must_use]
    pub const fn topic(&self) -> Option<Topic> {
        self.topic
    }
}

/// Feedback attached to one actual support-authored reply.
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Feedback {
    message_id: String,
    helpful: bool,
    received_at: u64,
}

impl Feedback {
    /// Support reply being rated.
    #[must_use]
    pub fn message_id(&self) -> &str {
        &self.message_id
    }

    /// Whether it helped.
    #[must_use]
    pub const fn helpful(&self) -> bool {
        self.helpful
    }

    /// Caller-injected feedback timestamp.
    #[must_use]
    pub const fn received_at(&self) -> u64 {
        self.received_at
    }
}

/// One durable conversation in an authenticated principal's scope.
#[allow(clippy::struct_field_names)]
#[derive(Clone, Debug, Deserialize, Eq, PartialEq, Serialize)]
pub struct Conversation {
    conversation_id: String,
    shell: Shell,
    state: ConversationState,
    created_at: u64,
    updated_at: u64,
    trace_id: Option<String>,
    messages: Vec<Message>,
    feedback: Vec<Feedback>,
}

impl Conversation {
    /// Stable conversation identifier.
    #[must_use]
    pub fn conversation_id(&self) -> &str {
        &self.conversation_id
    }

    /// Originating shell.
    #[must_use]
    pub const fn shell(&self) -> Shell {
        self.shell
    }

    /// Current state.
    #[must_use]
    pub const fn state(&self) -> ConversationState {
        self.state
    }

    /// Creation timestamp.
    #[must_use]
    pub const fn created_at(&self) -> u64 {
        self.created_at
    }

    /// Latest content timestamp.
    #[must_use]
    pub const fn updated_at(&self) -> u64 {
        self.updated_at
    }

    /// Attached consented trace and no other diagnostic context.
    #[must_use]
    pub fn trace_id(&self) -> Option<&str> {
        self.trace_id.as_deref()
    }

    /// Ordered messages.
    #[must_use]
    pub fn messages(&self) -> &[Message] {
        &self.messages
    }

    /// Recorded feedback.
    #[must_use]
    pub fn feedback(&self) -> &[Feedback] {
        &self.feedback
    }

    /// Count of unread support-authored replies.
    #[must_use]
    pub fn unread_count(&self) -> usize {
        self.messages
            .iter()
            .filter(|message| message.author == Author::Support && !message.read)
            .count()
    }
}

/// Validated input for a new conversation.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct CreateConversation {
    body: String,
    shell: Shell,
    topic: Option<Topic>,
    trace_id: Option<TraceId>,
}

impl CreateConversation {
    /// Builds a conversation without diagnostic context.
    ///
    /// # Errors
    ///
    /// Refuses empty, oversize or control-character message bodies.
    pub fn new(body: impl Into<String>, shell: Shell) -> Result<Self, SupportError> {
        Ok(Self {
            body: normalized_body(&body.into())?,
            shell,
            topic: None,
            trace_id: None,
        })
    }

    /// Seeds the first message with a declared topic.
    #[must_use]
    pub const fn with_topic(mut self, topic: Topic) -> Self {
        self.topic = Some(topic);
        self
    }

    /// Attaches only the consented error trace.
    #[must_use]
    pub fn with_trace(mut self, trace: TraceId) -> Self {
        self.trace_id = Some(trace);
        self
    }
}

/// Support-service failure with stable API classification.
#[derive(Debug)]
pub enum SupportError {
    /// Principal store failed.
    Store(StoreError),
    /// Body failed contract bounds.
    InvalidBody,
    /// Idempotency key failed contract bounds.
    InvalidIdempotencyKey,
    /// Conversation does not exist in this principal scope.
    ConversationUnknown,
    /// Message does not exist in this conversation.
    MessageUnknown,
    /// One idempotency identity was reused for different content.
    Conflict,
    /// Conversation reached its message bound.
    ConversationFull,
    /// Person tried to reply after resolution.
    ConversationResolved,
    /// Stored state is malformed.
    Corrupt,
}

impl SupportError {
    /// Stable human-api machine code.
    #[must_use]
    pub const fn machine_code(&self) -> &'static str {
        match self {
            Self::Store(_) => "support-unavailable",
            Self::InvalidBody => "support-message-invalid",
            Self::InvalidIdempotencyKey => "idempotency-key-invalid",
            Self::ConversationUnknown => "support-conversation-unknown",
            Self::MessageUnknown => "support-message-unknown",
            Self::Conflict => "support-idempotency-conflict",
            Self::ConversationFull => "support-conversation-full",
            Self::ConversationResolved => "support-conversation-resolved",
            Self::Corrupt => "support-state-corrupt",
        }
    }

    /// Whether a later request can safely retry without changing its identity.
    #[must_use]
    pub const fn retriable(&self) -> bool {
        matches!(self, Self::Store(_))
    }
}

impl Display for SupportError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(self.machine_code())
    }
}

impl std::error::Error for SupportError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            Self::Store(error) => Some(error),
            _ => None,
        }
    }
}

impl From<StoreError> for SupportError {
    fn from(value: StoreError) -> Self {
        Self::Store(value)
    }
}

/// Principal-scoped support operations used by the human-api transport.
#[derive(Clone, Copy, Debug, Default)]
pub struct SupportService;

impl SupportService {
    /// Lists the newest conversations for the authenticated principal.
    ///
    /// # Errors
    ///
    /// Refuses corrupt durable rows.
    pub fn list(scope: &PrincipalScope<'_>) -> Result<Vec<Conversation>, SupportError> {
        let mut conversations = scope
            .keys(Table::Support)
            .into_iter()
            .filter(|key| key.as_str().starts_with(ROW_PREFIX))
            .map(|key| load_required(scope, &key))
            .collect::<Result<Vec<_>, _>>()?;
        conversations.sort_by(|left, right| {
            right
                .updated_at
                .cmp(&left.updated_at)
                .then_with(|| right.conversation_id.cmp(&left.conversation_id))
        });
        conversations.truncate(MAX_CONVERSATIONS);
        Ok(conversations)
    }

    /// Creates one conversation, converging retries under the same key.
    ///
    /// # Errors
    ///
    /// Refuses malformed idempotency, conflicting replays and persistence failures.
    pub fn create(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        idempotency_key: &str,
        request: &CreateConversation,
    ) -> Result<Conversation, SupportError> {
        validate_idempotency_key(idempotency_key)?;
        let identity = digest_hex(ID_DOMAIN, scope.principal().as_str(), idempotency_key);
        let key = row_key(&identity)?;
        let request_digest = create_digest(request);
        if let Some(existing) = load(scope, &key)? {
            return if conversation_create_digest(&existing) == request_digest {
                Ok(existing)
            } else {
                Err(SupportError::Conflict)
            };
        }
        let conversation_id = format!("sup_{identity}");
        let message_id = message_id(&conversation_id, idempotency_key);
        let conversation = Conversation {
            conversation_id,
            shell: request.shell,
            state: ConversationState::WaitingForSupport,
            created_at: now,
            updated_at: now,
            trace_id: request
                .trace_id
                .as_ref()
                .map(|trace| trace.as_str().to_owned()),
            messages: vec![Message {
                message_id,
                author: Author::You,
                body: request.body.clone(),
                sent_at: now,
                read: true,
                topic: request.topic,
            }],
            feedback: Vec::new(),
        };
        persist(scope, now, &conversation)?;
        Ok(conversation)
    }

    /// Appends a person's reply with exactly-once retry identity.
    ///
    /// # Errors
    ///
    /// Refuses unknown, resolved or full conversations and conflicting retries.
    pub fn reply(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        conversation_id: &str,
        idempotency_key: &str,
        body: impl Into<String>,
    ) -> Result<Conversation, SupportError> {
        append(
            scope,
            now,
            conversation_id,
            idempotency_key,
            Author::You,
            &body.into(),
        )
    }

    /// Records a real support operator's reply. This is the only operation
    /// that can create a support-authored message.
    ///
    /// # Errors
    ///
    /// Refuses unknown or full conversations and conflicting retries.
    pub fn record_support_reply(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        conversation_id: &str,
        idempotency_key: &str,
        body: impl Into<String>,
    ) -> Result<Conversation, SupportError> {
        append(
            scope,
            now,
            conversation_id,
            idempotency_key,
            Author::Support,
            &body.into(),
        )
    }

    /// Marks support replies through the named message as read.
    ///
    /// # Errors
    ///
    /// Refuses unknown conversations/messages and persistence failures.
    pub fn mark_read(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        conversation_id: &str,
        through_message_id: &str,
    ) -> Result<Conversation, SupportError> {
        let key = key_for_conversation(conversation_id)?;
        let mut conversation = load_required(scope, &key)?;
        let boundary = conversation
            .messages
            .iter()
            .position(|message| message.message_id == through_message_id)
            .ok_or(SupportError::MessageUnknown)?;
        for message in conversation
            .messages
            .iter_mut()
            .take(boundary.saturating_add(1))
        {
            if message.author == Author::Support {
                message.read = true;
            }
        }
        persist(scope, now, &conversation)?;
        Ok(conversation)
    }

    /// Returns current status without mutating the conversation.
    ///
    /// # Errors
    ///
    /// Refuses unknown or corrupt conversation rows.
    pub fn status(
        scope: &PrincipalScope<'_>,
        conversation_id: &str,
    ) -> Result<(ConversationState, usize, u64), SupportError> {
        let conversation = load_required(scope, &key_for_conversation(conversation_id)?)?;
        Ok((
            conversation.state,
            conversation.unread_count(),
            conversation.updated_at,
        ))
    }

    /// Records feedback once for an actual support-authored message.
    ///
    /// # Errors
    ///
    /// Refuses unknown messages, conflicting duplicate feedback and persistence failures.
    pub fn feedback(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        conversation_id: &str,
        message_id: &str,
        helpful: bool,
    ) -> Result<Conversation, SupportError> {
        let key = key_for_conversation(conversation_id)?;
        let mut conversation = load_required(scope, &key)?;
        let message = conversation
            .messages
            .iter()
            .find(|message| message.message_id == message_id)
            .ok_or(SupportError::MessageUnknown)?;
        if message.author != Author::Support {
            return Err(SupportError::MessageUnknown);
        }
        if let Some(existing) = conversation
            .feedback
            .iter()
            .find(|entry| entry.message_id == message_id)
        {
            return if existing.helpful == helpful {
                Ok(conversation)
            } else {
                Err(SupportError::Conflict)
            };
        }
        conversation.feedback.push(Feedback {
            message_id: message_id.to_owned(),
            helpful,
            received_at: now,
        });
        persist(scope, now, &conversation)?;
        Ok(conversation)
    }

    /// Resolves a conversation from the support operator path.
    ///
    /// # Errors
    ///
    /// Refuses unknown conversations and persistence failures.
    pub fn resolve(
        scope: &mut PrincipalScope<'_>,
        now: u64,
        conversation_id: &str,
    ) -> Result<Conversation, SupportError> {
        let key = key_for_conversation(conversation_id)?;
        let mut conversation = load_required(scope, &key)?;
        conversation.state = ConversationState::Resolved;
        conversation.updated_at = now;
        persist(scope, now, &conversation)?;
        Ok(conversation)
    }
}

fn append(
    scope: &mut PrincipalScope<'_>,
    now: u64,
    conversation_id: &str,
    idempotency_key: &str,
    author: Author,
    body: &str,
) -> Result<Conversation, SupportError> {
    validate_idempotency_key(idempotency_key)?;
    let body = normalized_body(body)?;
    let key = key_for_conversation(conversation_id)?;
    let mut conversation = load_required(scope, &key)?;
    if author == Author::You && conversation.state == ConversationState::Resolved {
        return Err(SupportError::ConversationResolved);
    }
    let identifier = message_id(conversation_id, idempotency_key);
    if let Some(existing) = conversation
        .messages
        .iter()
        .find(|message| message.message_id == identifier)
    {
        return if existing.author == author && existing.body == body {
            Ok(conversation)
        } else {
            Err(SupportError::Conflict)
        };
    }
    if conversation.messages.len() >= MAX_MESSAGES {
        return Err(SupportError::ConversationFull);
    }
    conversation.messages.push(Message {
        message_id: identifier,
        author,
        body,
        sent_at: now,
        read: author == Author::You,
        topic: None,
    });
    conversation.state = match author {
        Author::You => ConversationState::WaitingForSupport,
        Author::Support => ConversationState::WaitingForYou,
    };
    conversation.updated_at = now;
    persist(scope, now, &conversation)?;
    Ok(conversation)
}

fn normalized_body(body: &str) -> Result<String, SupportError> {
    let normalized = body.trim();
    if normalized.is_empty()
        || normalized.chars().count() > MAX_BODY_CHARS
        || normalized
            .chars()
            .any(|character| character.is_control() && character != '\n' && character != '\t')
    {
        return Err(SupportError::InvalidBody);
    }
    Ok(normalized.to_owned())
}

fn validate_idempotency_key(value: &str) -> Result<(), SupportError> {
    if value.is_empty()
        || value.len() > MAX_IDEMPOTENCY_KEY_BYTES
        || value.bytes().any(|byte| byte.is_ascii_control())
    {
        Err(SupportError::InvalidIdempotencyKey)
    } else {
        Ok(())
    }
}

fn create_digest(request: &CreateConversation) -> String {
    creation_digest(
        &request.body,
        request.shell,
        request.topic,
        request.trace_id.as_ref().map(TraceId::as_str),
    )
}

fn conversation_create_digest(conversation: &Conversation) -> String {
    let first = conversation.messages.first();
    creation_digest(
        first.map_or("", |message| message.body.as_str()),
        conversation.shell,
        first.and_then(|message| message.topic),
        conversation.trace_id.as_deref(),
    )
}

fn creation_digest(
    body: &str,
    shell: Shell,
    topic: Option<Topic>,
    trace_id: Option<&str>,
) -> String {
    let mut digest = Sha256::new();
    digest.update(body.as_bytes());
    digest.update([shell as u8]);
    digest.update([topic.map_or(0, |value| value as u8).saturating_add(1)]);
    if let Some(trace) = trace_id {
        digest.update(trace.as_bytes());
    }
    hex_digest(digest.finalize())
}

fn digest_hex(domain: &[u8], principal: &str, value: &str) -> String {
    let mut digest = Sha256::new();
    digest.update(domain);
    digest.update(principal.as_bytes());
    digest.update([0]);
    digest.update(value.as_bytes());
    hex_digest(digest.finalize())
}

fn message_id(conversation_id: &str, idempotency_key: &str) -> String {
    format!(
        "msg_{}",
        digest_hex(MESSAGE_DOMAIN, conversation_id, idempotency_key)
    )
}

fn row_key(identity: &str) -> Result<RowKey, SupportError> {
    RowKey::new(format!("{ROW_PREFIX}{identity}")).map_err(SupportError::from)
}

fn key_for_conversation(conversation_id: &str) -> Result<RowKey, SupportError> {
    let identity = conversation_id
        .strip_prefix("sup_")
        .ok_or(SupportError::ConversationUnknown)?;
    if identity.len() != 64 || !identity.bytes().all(|byte| byte.is_ascii_hexdigit()) {
        return Err(SupportError::ConversationUnknown);
    }
    row_key(identity)
}

fn load(scope: &PrincipalScope<'_>, key: &RowKey) -> Result<Option<Conversation>, SupportError> {
    scope
        .get(Table::Support, key)
        .map(|row| serde_json::from_slice(row.bytes()).map_err(|_| SupportError::Corrupt))
        .transpose()
}

fn load_required(scope: &PrincipalScope<'_>, key: &RowKey) -> Result<Conversation, SupportError> {
    load(scope, key)?.ok_or(SupportError::ConversationUnknown)
}

fn persist(
    scope: &mut PrincipalScope<'_>,
    now: u64,
    conversation: &Conversation,
) -> Result<(), SupportError> {
    let bytes = serde_json::to_vec(conversation).map_err(|_| SupportError::Corrupt)?;
    scope.put(
        Table::Support,
        key_for_conversation(&conversation.conversation_id)?,
        now,
        bytes,
    )?;
    Ok(())
}

fn hex_digest(bytes: impl AsRef<[u8]>) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bytes = bytes.as_ref();
    let mut output = String::with_capacity(bytes.len().saturating_mul(2));
    for byte in bytes {
        output.push(char::from(HEX[usize::from(byte >> 4)]));
        output.push(char::from(HEX[usize::from(byte & 0x0f)]));
    }
    output
}
