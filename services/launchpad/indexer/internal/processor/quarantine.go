package processor

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/ethereum/go-ethereum/core/types"
)

type OptionalFamilyPolicy struct{ ABIRevision, Requirement string }
type QuarantinePolicy struct {
	Registry              *decode.Registry
	SourceStream          string
	OptionalEmitterTopics map[string]OptionalFamilyPolicy
}

func rawSourceRecord(log types.Log) (store.RawRecord, error) {
	if log.BlockNumber > math.MaxInt64 || log.TxIndex > math.MaxUint32 || log.Index > math.MaxUint32 || log.Removed {
		return store.RawRecord{}, store.ErrIntegrity
	}
	r := store.RawRecord{TransactionHash: strings.ToLower(log.TxHash.Hex()), BlockHash: strings.ToLower(log.BlockHash.Hex()), Emitter: strings.ToLower(log.Address.Hex()), Height: int64(log.BlockNumber), TransactionIndex: uint32(log.TxIndex), LogIndex: uint32(log.Index), Data: append([]byte(nil), log.Data...), Topics: make([]string, len(log.Topics))}
	for i, t := range log.Topics {
		r.Topics[i] = strings.ToLower(t.Hex())
	}
	_, err := store.RawDigest(r)
	return r, err
}
func (p *Processor) RetainSourcePoison(ctx context.Context, result source.FetchResult) error {
	if p == nil {
		return errors.New("processor unavailable")
	}
	policy := p.deps.Quarantine
	if p.deps.Store == nil || policy == nil || policy.Registry == nil || policy.SourceStream == "" {
		return errors.New("source quarantine requires approved registry and explicit policy")
	}
	var blocked error
	for _, log := range result.Logs {
		raw, err := rawSourceRecord(log)
		if err != nil {
			return err
		}
		decoded, decodeErr := policy.Registry.Decode(log)
		code := ""
		abi := "unavailable"
		required := true
		requirement := "D02_021"
		if decoded != nil {
			abi = decoded.ABIRevision
			if abi == "" {
				abi = "unavailable"
			}
			code = decoded.FailureReason
		}
		if decodeErr == nil && decoded != nil && decoded.Status == "decoded" {
			code = ""
		}
		if decodeErr != nil && code == "" {
			code = "decode_failure"
		}
		if decoded == nil {
			code = "decode_failure"
		}
		if len(log.Topics) > 0 {
			key := raw.Emitter + ":" + raw.Topics[0]
			if optional, ok := policy.OptionalEmitterTopics[key]; ok && optional.ABIRevision != "" && optional.Requirement != "" {
				required = false
				requirement = optional.Requirement
				if abi != "unavailable" && abi != optional.ABIRevision {
					return errors.New("optional classification ABI policy mismatch")
				}
				abi = optional.ABIRevision
			}
		}
		if result.BlockTimestamps[log.BlockNumber] <= 0 {
			code = "source_repair_required"
			required = true
		}
		if code == "" {
			continue
		}
		due, err := p.deps.Store.PoisonRetryDue(ctx, p.deps.ChainID, policy.SourceStream, raw.TransactionHash, raw.LogIndex)
		if err != nil {
			return err
		}
		if !due {
			if required {
				blocked = errors.Join(blocked, store.ErrRequiredPoison)
			}
			continue
		}
		record := store.PoisonRecord{ChainID: p.deps.ChainID, StreamID: policy.SourceStream, Raw: raw, ErrorCode: code, ABIRevision: abi, Requirement: requirement, Required: required}
		if err = p.deps.Store.RetainPoison(ctx, record); err != nil {
			return err
		}
		if required {
			blocked = errors.Join(blocked, fmt.Errorf("%w: %s at %d/%d", store.ErrRequiredPoison, code, raw.Height, raw.LogIndex))
		}
	}
	return blocked
}

type PrivateOperationAuthorizer interface {
	AuthorizeOperation(context.Context, store.OperationScope) error
}
type PrivateOperations struct {
	Store      *store.Store
	Authorizer PrivateOperationAuthorizer
}

func (o *PrivateOperations) authorize(ctx context.Context, scope store.OperationScope) error {
	if o == nil || o.Store == nil || o.Authorizer == nil {
		return errors.New("authenticated private operation authority required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return o.Authorizer.AuthorizeOperation(ctx, scope)
}
func (o *PrivateOperations) AdmitReplay(ctx context.Context, request store.ReplayRequest) error {
	if err := store.ValidateReplayRequest(request); err != nil {
		return err
	}
	if err := o.authorize(ctx, request.Scope()); err != nil {
		return err
	}
	return o.Store.AdmitReplay(ctx, request)
}
func (o *PrivateOperations) WaivePoison(ctx context.Context, scope store.OperationScope, rawDigest, requirement, reason string, expires time.Time) error {
	if scope.Action != "waive" {
		return errors.New("waiver action required")
	}
	if err := o.authorize(ctx, scope); err != nil {
		return err
	}
	return o.Store.WaivePoison(ctx, scope, rawDigest, requirement, reason, expires)
}

type DatabaseOperationAuthorizer struct {
	Store                 *store.Store
	AuthenticatedOperator string
}

func (a *DatabaseOperationAuthorizer) AuthorizeOperation(ctx context.Context, scope store.OperationScope) error {
	if a == nil || a.Store == nil || a.AuthenticatedOperator == "" || scope.Operator != a.AuthenticatedOperator {
		return errors.New("operation requester differs from authenticated operator")
	}
	return a.Store.CheckOperationAuthorization(ctx, scope)
}
