package control

import "time"

type ActionRequest struct {
	Key         string    `json:"key"`
	Kind        string    `json:"kind"`
	Scope       string    `json:"scope"`
	Reason      string    `json:"reason"`
	PayloadHash string    `json:"payloadHash"`
	Expires     time.Time `json:"expiresAt"`
}
type TransitionRequest struct {
	Key      string  `json:"key"`
	Scope    string  `json:"scope"`
	Reason   string  `json:"reason"`
	Next     string  `json:"next"`
	Expected *uint64 `json:"expectedRevision"`
}
type CapRequest struct {
	Scope       string  `json:"scope"`
	Reason      string  `json:"reason"`
	ApprovalKey string  `json:"approvalKey"`
	Cap         Cap     `json:"cap"`
	Expected    *uint64 `json:"expectedRevision"`
}
type BudgetRequest struct {
	Reason   string  `json:"reason"`
	Budget   Budget  `json:"budget"`
	Expected *uint64 `json:"expectedRevision"`
}
type PolicyRequest struct {
	Reason string `json:"reason"`
	Policy Policy `json:"policy"`
}
type RecommendationRequest struct {
	PolicyID   string    `json:"policyId"`
	Key        string    `json:"key"`
	Instrument string    `json:"instrument"`
	Reason     string    `json:"reason"`
	QuoteAt    time.Time `json:"quoteAt"`
}
type BountyRequest struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
	Bounty Bounty `json:"bounty"`
}
type BountyTransitionRequest struct {
	TransitionRequest
	Evidence    string `json:"evidence"`
	Destination string `json:"destination"`
}
type InventoryRequest struct {
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
	Asset  Asset  `json:"asset"`
}
type AccessRequest struct {
	Scope       string `json:"scope"`
	Reason      string `json:"reason"`
	Dataset     string `json:"dataset"`
	ApprovalKey string `json:"approvalKey"`
	Bulk        bool   `json:"bulk"`
}
type IncidentRequest struct {
	Scope    string `json:"scope"`
	Key      string `json:"key"`
	Reason   string `json:"reason"`
	Identity string `json:"identity"`
	Evidence string `json:"evidence"`
}
type SettlementRequest struct {
	Scope  string `json:"scope"`
	Key    string `json:"key"`
	Reason string `json:"reason"`
}
type WorkflowEnvelope struct {
	RequestID   string `json:"requestId"`
	Data        any    `json:"data"`
	Quality     string `json:"quality"`
	GeneratedAt string `json:"generatedAt"`
}
type WorkflowError struct {
	Code      string `json:"code"`
	RequestID string `json:"requestId"`
	Rule      string `json:"rule"`
	Retryable bool   `json:"retryable"`
}

type ActionCompletionRequest struct {
	TransitionRequest
	Reference string `json:"reference"`
}
type IncidentRecoveryRequest struct {
	TransitionRequest
	ApprovalKey string `json:"approvalKey"`
	Rotation    string `json:"rotation"`
	Integrity   string `json:"integrity"`
}
type SubmissionRequest struct {
	Scope      string     `json:"scope"`
	Reason     string     `json:"reason"`
	Submission Submission `json:"submission"`
}
