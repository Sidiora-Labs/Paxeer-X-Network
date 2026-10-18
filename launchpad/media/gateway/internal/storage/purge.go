package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type PurgeReceipt struct {
	OperationID     string `json:"operationId"`
	ProviderID      string `json:"providerId"`
	State           string `json:"state"`
	CompletionProof string `json:"completionProof"`
}
type PurgeProvider interface {
	Submit(context.Context, string, string) (PurgeReceipt, error)
	Status(context.Context, string, string) (PurgeReceipt, error)
}
type HTTPPurgeProvider struct {
	endpoint string
	token    string
	client   *http.Client
}

func NewHTTPPurgeProvider(endpoint, token string, transport *http.Transport) (*HTTPPurgeProvider, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || len(token) < 32 || strings.ContainsAny(token, "\r\n") {
		return nil, ErrUnavailable
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	if transport != nil {
		t = transport.Clone()
	}
	if t.TLSClientConfig != nil && t.TLSClientConfig.InsecureSkipVerify {
		return nil, ErrUnavailable
	}
	t.Proxy = nil
	t.ResponseHeaderTimeout = 3 * time.Second
	return &HTTPPurgeProvider{strings.TrimRight(endpoint, "/"), token, &http.Client{Transport: t, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (p *HTTPPurgeProvider) call(ctx context.Context, path string, request any) (PurgeReceipt, error) {
	var receipt PurgeReceipt
	body, e := json.Marshal(request)
	if e != nil {
		return receipt, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", p.endpoint+path, bytes.NewReader(body))
	if e != nil {
		return receipt, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Content-Type", "application/json")
	if submission, ok := request.(purgeSubmission); ok {
		req.Header.Set("Idempotency-Key", submission.OperationID)
	}
	resp, e := p.client.Do(req)
	if e != nil {
		return receipt, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 202 {
		return receipt, ErrUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if e != nil || len(raw) > 16384 {
		return receipt, ErrUnavailable
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&receipt) != nil || d.Decode(new(any)) != io.EOF || !atom.MatchString(receipt.ProviderID) || len(receipt.CompletionProof) > 256 {
		return PurgeReceipt{}, ErrUnavailable
	}
	if receipt.State != "accepted" && receipt.State != "complete" && receipt.State != "failed" {
		return PurgeReceipt{}, ErrUnavailable
	}
	if receipt.State == "complete" && receipt.CompletionProof == "" {
		return PurgeReceipt{}, ErrUnavailable
	}
	return receipt, nil
}

type purgeSubmission struct {
	OperationID string `json:"operationId"`
	Hash        string `json:"hash"`
}

func (p *HTTPPurgeProvider) Submit(ctx context.Context, id, hash string) (PurgeReceipt, error) {
	if !atom.MatchString(id) || !digest.MatchString(hash) {
		return PurgeReceipt{}, ErrConflict
	}
	receipt, e := p.call(ctx, "/v1/purges", purgeSubmission{id, hash})
	if e == nil && (receipt.OperationID != id || receipt.State == "complete") {
		return PurgeReceipt{}, ErrUnavailable
	}
	return receipt, e
}
func (p *HTTPPurgeProvider) Status(ctx context.Context, id, providerID string) (PurgeReceipt, error) {
	if !atom.MatchString(id) || !atom.MatchString(providerID) {
		return PurgeReceipt{}, ErrConflict
	}
	receipt, e := p.call(ctx, "/v1/purges/status", struct {
		OperationID string `json:"operationId"`
		ProviderID  string `json:"providerId"`
	}{id, providerID})
	if e == nil && (receipt.OperationID != id || receipt.ProviderID != providerID) {
		return PurgeReceipt{}, ErrUnavailable
	}
	return receipt, e
}

type PurgeWorker struct {
	Authority *Authority
	Provider  PurgeProvider
}

func (w PurgeWorker) RunOne(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if w.Authority == nil || w.Provider == nil {
		return false, ErrUnavailable
	}
	a := w.Authority
	tx, e := a.Pool.Begin(ctx)
	if e != nil {
		return false, ErrUnavailable
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var op Operation
	e = tx.QueryRow(ctx, "SELECT id::text,hash,revision,state,provider_id FROM "+a.table("operations")+" WHERE kind='purge' AND state IN ('requested','accepted') ORDER BY requested_at,id LIMIT 1 FOR UPDATE SKIP LOCKED").Scan(&op.ID, &op.Hash, &op.Revision, &op.State, &op.ProviderID)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var receipt PurgeReceipt
	if op.State == "requested" && op.ProviderID == "" {
		receipt, e = w.Provider.Submit(bounded, op.ID, op.Hash)
	} else {
		receipt, e = w.Provider.Status(bounded, op.ID, op.ProviderID)
	}
	if e != nil {
		if _, updateErr := tx.Exec(ctx, "UPDATE "+a.table("operations")+" SET attempts=attempts+1,updated_at=clock_timestamp() WHERE id=$1", op.ID); updateErr != nil {
			return true, ErrUnavailable
		}
		if commitErr := tx.Commit(ctx); commitErr != nil {
			return true, ErrUnavailable
		}
		return true, e
	}
	if receipt.OperationID != op.ID || !atom.MatchString(receipt.ProviderID) || len(receipt.CompletionProof) > 256 || (receipt.State != "accepted" && receipt.State != "complete" && receipt.State != "failed") || (op.State == "requested" && op.ProviderID == "" && receipt.State == "complete") || (op.ProviderID != "" && receipt.ProviderID != op.ProviderID) || (receipt.State == "complete" && receipt.CompletionProof == "") {
		return true, ErrConflict
	}
	if _, e = tx.Exec(ctx, "UPDATE "+a.table("operations")+" SET state=$2,provider_id=$3,proof=$4,attempts=attempts+1,updated_at=clock_timestamp() WHERE id=$1", op.ID, receipt.State, receipt.ProviderID, receipt.CompletionProof); e != nil {
		return true, ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return true, ErrUnavailable
	}
	return true, nil
}

func (a *Authority) PurgeBacklogAge(ctx context.Context) (time.Duration, error) {
	var seconds float64
	if e := a.Pool.QueryRow(ctx, "SELECT COALESCE(EXTRACT(EPOCH FROM(clock_timestamp()-min(requested_at))),0)::float8 FROM "+a.table("operations")+" WHERE kind='purge' AND state<>'complete'").Scan(&seconds); e != nil {
		return 0, ErrUnavailable
	}
	if seconds < 0 {
		return 0, ErrConflict
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

func (a *Authority) RetryPurge(ctx context.Context, id string) error {
	if !atom.MatchString(id) {
		return ErrConflict
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, e := a.Pool.Exec(ctx, "UPDATE "+a.table("operations")+" SET state='requested',proof='',updated_at=clock_timestamp() WHERE id=$1 AND kind='purge' AND state='failed'", id)
	if e != nil {
		return ErrUnavailable
	}
	if result.RowsAffected() != 1 {
		return ErrConflict
	}
	return nil
}
