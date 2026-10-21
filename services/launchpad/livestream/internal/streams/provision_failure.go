package streams

import (
	"context"
	"fmt"
	"time"
)

type ProvisionFailureReason string

const (
	ProvisionConfigurationUnavailable ProvisionFailureReason = "provision_configuration_unavailable"
	ProvisionQuotaUnavailable         ProvisionFailureReason = "provision_quota_unavailable"
	ProvisionPolicyRevoked            ProvisionFailureReason = "provision_policy_revoked"
)

func (s *LifecycleStore) FailBeforeProvision(ctx context.Context, id string, revision int64, reason ProvisionFailureReason) (Operation, error) {
	if !identityPattern.MatchString(id) || revision < 1 {
		return Operation{}, ErrInvalid
	}
	switch reason {
	case ProvisionConfigurationUnavailable, ProvisionQuotaUnavailable, ProvisionPolicyRevoked:
	default:
		return Operation{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Operation{}, err
	}
	defer rollbackBounded(tx)
	o, err := readOperation(tx.QueryRow(ctx, fmt.Sprintf("SELECT "+opColumns+" FROM %s.stream_operations WHERE id=$1 FOR UPDATE", s.schema), id))
	if err != nil {
		return Operation{}, err
	}
	var recordedReason string
	if o.State == "FAILED" && o.Revision == revision+1 && o.ProviderID == "" {
		err = tx.QueryRow(ctx, fmt.Sprintf("SELECT last_reason FROM %s.stream_operations WHERE id=$1", s.schema), id).Scan(&recordedReason)
		if err != nil {
			return Operation{}, err
		}
		if recordedReason == string(reason) {
			return o, tx.Commit(ctx)
		}
	}
	if o.State != "REQUESTED" || o.ProviderID != "" || o.Revision != revision {
		return Operation{}, ErrConflict
	}
	o, err = readOperation(tx.QueryRow(ctx, fmt.Sprintf("UPDATE %s.stream_operations SET state='FAILED',last_reason=$2,retry_at=NULL,revision=revision+1,terminal_at=clock_timestamp() WHERE id=$1 RETURNING "+opColumns, s.schema), id, string(reason)))
	if err != nil {
		return Operation{}, err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s.stream_audit(operation_id,actor,reason,revision) VALUES($1,'provisioner',$2,$3)", s.schema), id, string(reason), o.Revision)
	if err != nil {
		return Operation{}, err
	}
	return o, tx.Commit(ctx)
}
