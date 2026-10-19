use std::collections::{BTreeMap, BTreeSet, VecDeque};

/// Strict outbound scheduling order. Lower variants always dispatch first.
#[derive(Clone, Copy, Debug, Eq, Ord, PartialEq, PartialOrd)]
pub enum Priority {
    Submission,
    ReceiptResolution,
    Retry,
    InteractiveRead,
    Backfill,
    SubscriptionCatchUp,
    BulkRead,
}

impl Priority {
    pub const ALL: [Self; 7] = [
        Self::Submission,
        Self::ReceiptResolution,
        Self::Retry,
        Self::InteractiveRead,
        Self::Backfill,
        Self::SubscriptionCatchUp,
        Self::BulkRead,
    ];
}

/// Independent finite bounds for one scheduling lane.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub struct QueueBound {
    pub requests: usize,
    pub bytes: usize,
}

/// Complete boundary admission configuration.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AdmissionConfig {
    pub maximum_in_flight: usize,
    pub maximum_message_bytes: usize,
    pub lanes: BTreeMap<Priority, QueueBound>,
}

impl AdmissionConfig {
    /// Creates a configuration only when every priority lane has finite bounds.
    ///
    /// # Errors
    ///
    /// Returns `InvalidConfiguration` for a zero in-flight or message-byte maximum, or when any of
    /// the seven priorities is missing a lane or carries a zero request or byte bound.
    pub fn new(
        maximum_in_flight: usize,
        maximum_message_bytes: usize,
        lanes: impl IntoIterator<Item = (Priority, QueueBound)>,
    ) -> Result<Self, AdmissionError> {
        let lanes = lanes.into_iter().collect::<BTreeMap<_, _>>();
        if maximum_in_flight == 0
            || maximum_message_bytes == 0
            || lanes.len() != Priority::ALL.len()
            || Priority::ALL.iter().any(|priority| {
                lanes
                    .get(priority)
                    .is_none_or(|bound| bound.requests == 0 || bound.bytes == 0)
            })
        {
            return Err(AdmissionError::InvalidConfiguration);
        }
        Ok(Self {
            maximum_in_flight,
            maximum_message_bytes,
            lanes,
        })
    }
}

/// One caller-owned unit of outbound core work.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct BoundaryWork {
    pub request_id: u64,
    pub tenant: String,
    pub priority: Priority,
    pub bytes: Vec<u8>,
}

/// Current core signal observed at the admission boundary.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CoreAvailability {
    Ready,
    Backpressured { retry_after_ms: u64 },
    Unavailable { retry_after_ms: u64 },
}

/// Completion signal for dispatched work.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum CoreOutcome {
    Completed,
    Backpressured { retry_after_ms: u64 },
    Unavailable { retry_after_ms: u64 },
}

/// Exact source of caller-visible boundary backpressure.
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub enum BackpressureSource {
    QueueRequests,
    QueueBytes,
    InFlight,
    CoreBackpressured,
    CoreUnavailable,
}

/// Typed immediate refusal; the controller never waits or queues implicitly.
#[derive(Clone, Debug, Eq, PartialEq)]
pub enum AdmissionError {
    InvalidConfiguration,
    InvalidWork,
    DuplicateRequest(u64),
    UnknownInFlight(u64),
    Backpressure {
        source: BackpressureSource,
        priority: Priority,
        retry_after_ms: Option<u64>,
        queued_requests: usize,
        queued_bytes: usize,
    },
}

#[derive(Clone, Copy, Debug, Default, Eq, PartialEq)]
pub struct LaneUtilization {
    pub queued_requests: usize,
    pub queued_bytes: usize,
}

/// Work removed from the queue and owned by one boundary call.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct Dispatch {
    pub work: BoundaryWork,
    pub in_flight: usize,
}

/// Bounded, priority-ordered admission controller for all outbound core work.
#[derive(Debug)]
pub struct BoundaryAdmission {
    config: AdmissionConfig,
    queues: BTreeMap<Priority, VecDeque<BoundaryWork>>,
    utilization: BTreeMap<Priority, LaneUtilization>,
    queued_ids: BTreeSet<u64>,
    in_flight: BTreeMap<u64, Priority>,
}

impl BoundaryAdmission {
    #[must_use]
    pub fn new(config: AdmissionConfig) -> Self {
        let queues = Priority::ALL
            .into_iter()
            .map(|priority| (priority, VecDeque::new()))
            .collect();
        let utilization = Priority::ALL
            .into_iter()
            .map(|priority| (priority, LaneUtilization::default()))
            .collect();
        Self {
            config,
            queues,
            utilization,
            queued_ids: BTreeSet::new(),
            in_flight: BTreeMap::new(),
        }
    }

    /// Admits work into its independently bounded lane only while core is ready.
    ///
    /// # Errors
    ///
    /// Returns `InvalidWork` for an empty or oversized tenant or payload, `DuplicateRequest` for an
    /// identifier already queued or in flight, and `Backpressure` sourced from `CoreBackpressured`,
    /// `CoreUnavailable`, `QueueRequests` or `QueueBytes`.
    pub fn admit(
        &mut self,
        work: BoundaryWork,
        availability: CoreAvailability,
    ) -> Result<(), AdmissionError> {
        self.validate_work(&work)?;
        self.propagate_availability(work.priority, availability)?;
        if self.queued_ids.contains(&work.request_id)
            || self.in_flight.contains_key(&work.request_id)
        {
            return Err(AdmissionError::DuplicateRequest(work.request_id));
        }
        let bound = self.config.lanes[&work.priority];
        let current = self.utilization[&work.priority];
        if current.queued_requests >= bound.requests {
            return Err(backpressure(
                BackpressureSource::QueueRequests,
                work.priority,
                None,
                current,
            ));
        }
        let next_bytes = current
            .queued_bytes
            .checked_add(work.bytes.len())
            .ok_or_else(|| {
                backpressure(BackpressureSource::QueueBytes, work.priority, None, current)
            })?;
        if next_bytes > bound.bytes {
            return Err(backpressure(
                BackpressureSource::QueueBytes,
                work.priority,
                None,
                current,
            ));
        }

        let request_id = work.request_id;
        let priority = work.priority;
        self.queues.entry(priority).or_default().push_back(work);
        self.queued_ids.insert(request_id);
        self.utilization.insert(
            priority,
            LaneUtilization {
                queued_requests: current.queued_requests + 1,
                queued_bytes: next_bytes,
            },
        );
        Ok(())
    }

    /// Dispatches the oldest request from the strict highest-priority lane.
    ///
    /// # Errors
    ///
    /// Returns `Backpressure` sourced from `CoreBackpressured` or `CoreUnavailable` for a core that
    /// is not ready, or from `InFlight` once the in-flight maximum is reached; empty queues are
    /// `Ok(None)` rather than an error.
    pub fn dispatch(
        &mut self,
        availability: CoreAvailability,
    ) -> Result<Option<Dispatch>, AdmissionError> {
        let next_priority = Priority::ALL
            .into_iter()
            .find(|priority| !self.queues[priority].is_empty());
        let Some(priority) = next_priority else {
            return Ok(None);
        };
        self.propagate_availability(priority, availability)?;
        if self.in_flight.len() >= self.config.maximum_in_flight {
            return Err(backpressure(
                BackpressureSource::InFlight,
                priority,
                None,
                self.utilization[&priority],
            ));
        }

        let Some(work) = self.queues.entry(priority).or_default().pop_front() else {
            return Ok(None);
        };
        let current = self.utilization[&priority];
        self.utilization.insert(
            priority,
            LaneUtilization {
                queued_requests: current.queued_requests - 1,
                queued_bytes: current.queued_bytes - work.bytes.len(),
            },
        );
        self.queued_ids.remove(&work.request_id);
        self.in_flight.insert(work.request_id, priority);
        Ok(Some(Dispatch {
            work,
            in_flight: self.in_flight.len(),
        }))
    }

    /// Releases one boundary slot and propagates the exact core outcome.
    /// Failed work is caller-owned and is never silently requeued.
    ///
    /// # Errors
    ///
    /// Returns `UnknownInFlight` for an identifier this controller never dispatched, or
    /// `Backpressure` carrying the core's `retry_after_ms` for a `Backpressured` or `Unavailable`
    /// outcome; the in-flight slot is released before either refusal.
    pub fn finish(&mut self, request_id: u64, outcome: CoreOutcome) -> Result<(), AdmissionError> {
        let priority = self
            .in_flight
            .remove(&request_id)
            .ok_or(AdmissionError::UnknownInFlight(request_id))?;
        match outcome {
            CoreOutcome::Completed => Ok(()),
            CoreOutcome::Backpressured { retry_after_ms } => Err(backpressure(
                BackpressureSource::CoreBackpressured,
                priority,
                Some(retry_after_ms),
                self.utilization[&priority],
            )),
            CoreOutcome::Unavailable { retry_after_ms } => Err(backpressure(
                BackpressureSource::CoreUnavailable,
                priority,
                Some(retry_after_ms),
                self.utilization[&priority],
            )),
        }
    }

    #[must_use]
    pub fn utilization(&self, priority: Priority) -> LaneUtilization {
        self.utilization[&priority]
    }

    #[must_use]
    pub fn in_flight(&self) -> usize {
        self.in_flight.len()
    }

    fn validate_work(&self, work: &BoundaryWork) -> Result<(), AdmissionError> {
        if work.tenant.is_empty()
            || work.tenant.len() > 255
            || work.tenant.as_bytes().contains(&0)
            || work.bytes.is_empty()
            || work.bytes.len() > self.config.maximum_message_bytes
        {
            Err(AdmissionError::InvalidWork)
        } else {
            Ok(())
        }
    }

    fn propagate_availability(
        &self,
        priority: Priority,
        availability: CoreAvailability,
    ) -> Result<(), AdmissionError> {
        let current = self.utilization[&priority];
        match availability {
            CoreAvailability::Ready => Ok(()),
            CoreAvailability::Backpressured { retry_after_ms } => Err(backpressure(
                BackpressureSource::CoreBackpressured,
                priority,
                Some(retry_after_ms),
                current,
            )),
            CoreAvailability::Unavailable { retry_after_ms } => Err(backpressure(
                BackpressureSource::CoreUnavailable,
                priority,
                Some(retry_after_ms),
                current,
            )),
        }
    }
}

fn backpressure(
    source: BackpressureSource,
    priority: Priority,
    retry_after_ms: Option<u64>,
    current: LaneUtilization,
) -> AdmissionError {
    AdmissionError::Backpressure {
        source,
        priority,
        retry_after_ms,
        queued_requests: current.queued_requests,
        queued_bytes: current.queued_bytes,
    }
}
