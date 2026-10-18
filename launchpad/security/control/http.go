package control

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPService struct {
	store  *Store
	worker *SettlementWorker
}

func NewHTTPService(store *Store, worker *SettlementWorker) (*HTTPService, error) {
	if store == nil || store.pool == nil || store.identity == nil || (worker != nil && worker.store != store) {
		return nil, Invalid
	}
	return &HTTPService{store, worker}, nil
}
func (s *HTTPService) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v2/control/caps/{capability}", s.cap)
	mux.HandleFunc("POST /v2/control/{operation}", s.mutate)
	mux.HandleFunc("GET /v2/control/campaigns/{policy}", s.campaign)
	mux.HandleFunc("GET /v2/control/recommendations/{key}", s.recommendation)
	mux.HandleFunc("GET /v2/control/settlement-capability", s.settlementCapability)
	return mux
}
func decodeRequest(r *http.Request, out any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return Invalid
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	if err != nil || len(raw) > 65536 {
		return Invalid
	}
	if rejectDuplicateFields(raw) != nil {
		return Invalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return Invalid
	}
	return nil
}
func respond(w http.ResponseWriter, data any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	requestID := id()
	if err != nil {
		var fault Fault
		if !errors.As(err, &fault) {
			fault = Unavailable
		}
		status, code := ErrorResponse(fault)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(WorkflowError{code, requestID, string(fault), status == 503})
		return
	}
	_ = json.NewEncoder(w).Encode(WorkflowEnvelope{requestID, data, "complete", time.Now().UTC().Format("2006-01-02T15:04:05.000Z")})
}
func credential(r *http.Request) (string, error) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || fields[0] != "Bearer" || len(fields[1]) > 4096 {
		return "", Denied
	}
	return fields[1], nil
}
func (s *HTTPService) cap(w http.ResponseWriter, r *http.Request) {
	d, err := s.store.Admission(r.Context(), r.PathValue("capability"), 1)
	if err != nil {
		d = CapDecision{Capability: r.PathValue("capability"), Stale: true, Reason: "cap_unavailable"}
	}
	respond(w, d.Warning(time.Now()), nil)
}
func (s *HTTPService) mutate(w http.ResponseWriter, r *http.Request) {
	cred, err := credential(r)
	if err != nil {
		unauthenticated(w)
		return
	}
	if _, err = s.store.identity.Resolve(r.Context(), cred); err != nil {
		unauthenticated(w)
		return
	}
	var data any
	switch r.PathValue("operation") {
	case "replays":
		var q ReplayProposal
		err = decodeRequest(r, &q)
		if err == nil {
			data, err = s.store.ProposeReplay(r.Context(), cred, q)
		}
	case "actions":
		var q ActionRequest
		err = decodeRequest(r, &q)
		if err == nil {
			data, err = s.store.Propose(r.Context(), cred, q.Key, q.Kind, q.Scope, q.Reason, q.PayloadHash, q.Expires)
		}
	case "complete-action":
		var q ActionCompletionRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			err = s.store.CompleteAction(r.Context(), cred, q.Key, q.Scope, q.Reason, q.Next, q.Reference, *q.Expected, DurableJobResults{s.store})
		}
	case "recover-incident":
		var q IncidentRecoveryRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			err = s.store.RecoverIncident(r.Context(), cred, q.Scope, q.Key, q.Reason, q.ApprovalKey, q.Rotation, q.Integrity, *q.Expected, IncidentRecovery{s.store})
		}
	case "bounty-submissions":
		var q SubmissionRequest
		err = decodeRequest(r, &q)
		if err == nil {
			var actor Principal
			actor, err = s.store.identity.Resolve(r.Context(), cred)
			if err == nil {
				err = s.store.SubmitBounty(r.Context(), cred, actor.ID, q.Scope, q.Reason, q.Submission)
			}
		}
	case "transition":
		var q TransitionRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			data, err = s.store.TransitionWorkflow(r.Context(), cred, q)
		}
	case "caps":
		var q CapRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			err = s.store.SetCap(r.Context(), cred, q.Scope, q.Reason, q.ApprovalKey, q.Cap, *q.Expected)
			data = map[string]bool{"updated": err == nil}
		}
	case "budget":
		var q BudgetRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			var actor Principal
			actor, err = s.store.identity.Resolve(r.Context(), cred)
			if err == nil {
				err = s.store.SetBudget(r.Context(), cred, actor.ID, q.Reason, q.Budget, *q.Expected)
			}
		}
	case "policies":
		var q PolicyRequest
		err = decodeRequest(r, &q)
		if err == nil {
			var actor Principal
			actor, err = s.store.identity.Resolve(r.Context(), cred)
			if err == nil {
				err = s.store.CreatePolicy(r.Context(), cred, actor.ID, q.Reason, q.Policy)
			}
		}
	case "policy-state":
		var q TransitionRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			var actor Principal
			actor, err = s.store.identity.Resolve(r.Context(), cred)
			if err == nil {
				err = s.store.PolicyState(r.Context(), cred, actor.ID, q.Key, q.Reason, q.Next, *q.Expected)
			}
		}
	case "recommendations":
		var q RecommendationRequest
		err = decodeRequest(r, &q)
		if err == nil {
			var actor Principal
			actor, err = s.store.identity.Resolve(r.Context(), cred)
			if err == nil {
				data, err = s.store.ReserveRecommendation(r.Context(), cred, actor.ID, q.PolicyID, q.Key, q.Instrument, q.Reason, q.QuoteAt)
			}
		}
	case "bounties":
		var q BountyRequest
		err = decodeRequest(r, &q)
		if err == nil {
			err = s.store.CreateBounty(r.Context(), cred, q.Scope, q.Reason, q.Bounty)
		}
	case "bounty-state":
		var q BountyTransitionRequest
		err = decodeRequest(r, &q)
		if err == nil && q.Expected == nil {
			err = Invalid
		}
		if err == nil {
			err = s.store.BountyState(r.Context(), cred, q.Scope, q.Key, q.Reason, q.Next, q.Evidence, q.Destination, *q.Expected)
		}
	case "inventory":
		var q InventoryRequest
		err = decodeRequest(r, &q)
		if err == nil {
			err = s.store.Reconcile(r.Context(), cred, q.Scope, q.Reason, q.Asset)
		}
	case "access":
		var q AccessRequest
		err = decodeRequest(r, &q)
		if err == nil {
			err = s.store.Access(r.Context(), cred, q.Scope, q.Reason, q.Dataset, q.ApprovalKey, q.Bulk)
		}
	case "incidents":
		var q IncidentRequest
		err = decodeRequest(r, &q)
		if err == nil {
			err = s.store.DetectIncident(r.Context(), cred, q.Scope, q.Key, q.Reason, q.Identity, q.Evidence)
		}
	case "settlements":
		var q SettlementRequest
		err = decodeRequest(r, &q)
		if err == nil {
			if s.worker == nil {
				err = Unavailable
			} else {
				data, err = s.worker.Process(r.Context(), cred, q.Scope, q.Key, q.Reason)
			}
		}
	default:
		err = Missing
	}
	respond(w, data, err)
}

func (s *HTTPService) campaign(w http.ResponseWriter, r *http.Request) {
	cred, err := credential(r)
	if err != nil {
		unauthenticated(w)
		return
	}
	if _, err = s.store.identity.Resolve(r.Context(), cred); err != nil {
		unauthenticated(w)
		return
	}
	actor, err := s.store.identity.Resolve(r.Context(), cred)
	if err != nil {
		respond(w, nil, err)
		return
	}
	view, err := s.store.Campaign(r.Context(), cred, actor.ID, r.PathValue("policy"), "owner requested automation status")
	respond(w, view, err)
}

func rejectDuplicateFields(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 16 {
			return Invalid
		}
		token, err := d.Token()
		if err != nil {
			return Invalid
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				token, err := d.Token()
				if err != nil {
					return Invalid
				}
				name, ok := token.(string)
				if !ok || seen[strings.ToLower(name)] {
					return Invalid
				}
				seen[strings.ToLower(name)] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return Invalid
		}
		_, err = d.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return Invalid
	}
	return nil
}

func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(WorkflowError{Code: "AUTH_REQUIRED", RequestID: id(), Rule: "authentication_required"})
}
func (s *HTTPService) recommendation(w http.ResponseWriter, r *http.Request) {
	cred, err := credential(r)
	if err != nil {
		unauthenticated(w)
		return
	}
	actor, err := s.store.identity.Resolve(r.Context(), cred)
	if err != nil {
		unauthenticated(w)
		return
	}
	view, err := s.store.Recommendation(r.Context(), cred, actor.ID, "owner requested disclosed recommendation", r.PathValue("key"))
	respond(w, view, err)
}
func (s *HTTPService) settlementCapability(w http.ResponseWriter, r *http.Request) {
	type capabilityView struct {
		Available              bool     `json:"available"`
		Blockers               []string `json:"blockers"`
		Controller             string   `json:"controller,omitempty"`
		AuthorityReference     string   `json:"authorityReference,omitempty"`
		CreationProtocolFeeRaw string   `json:"creationProtocolFeeRaw"`
		FeeAsset               Asset    `json:"feeAsset"`
		GasIncluded            bool     `json:"gasIncluded"`
	}
	view := capabilityView{Blockers: []string{"settlement_authority_unavailable"}, CreationProtocolFeeRaw: "1000000", FeeAsset: Asset{Chain: 125, Address: USDLAddress, Decimals: 6}}
	if s.worker != nil && s.store.verifier != nil {
		view.Available = true
		view.Blockers = []string{}
		view.Controller = s.worker.provider.Controller
		view.AuthorityReference = s.worker.provider.AuthorityReference
	}
	respond(w, view, nil)
}
