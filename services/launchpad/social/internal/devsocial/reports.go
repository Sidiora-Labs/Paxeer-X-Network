package devsocial

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Reports (IF_REPORT_CREATE) over chat.v2_reports. Filing a report returns a
// tracking id and never hides content by itself; the target must exist for
// messages and comments and be well-formed for profiles, tokens and streams.

var addressRE = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

var reportReasons = map[string]bool{"spam": true, "abuse": true, "fraud": true, "copyright": true, "other": true}

func (rt *Runtime) createReport(w http.ResponseWriter, r *http.Request) {
	wallet, ok := actor(r)
	if !ok {
		storeError(w, r, errUnauthenticated)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		storeError(w, r, err)
		return
	}
	var body v2.ReportCreateBody
	if !decode(w, r, "ReportCreateBody", &body) {
		return
	}
	reason := strings.ToLower(strings.TrimSpace(body.Reason))
	if !reportReasons[reason] {
		reason = "other"
	}
	if !plainText(body.Reason) || (body.Evidence != "" && !plainText(body.Evidence)) {
		storeError(w, r, errInvalid)
		return
	}
	target := strings.TrimSpace(body.TargetId)
	if body.TargetType == "profile" || body.TargetType == "token" {
		target = strings.ToLower(target)
	}
	report, err := rt.insertReport(r.Context(), wallet, key, body.TargetType, target, reason, body.Reason, body.Evidence)
	if err != nil {
		storeError(w, r, err)
		return
	}
	writeData(w, r, http.StatusCreated, "ReportCreateResponse", v2.ReportCreateData{Report: report}, nil)
}

func (rt *Runtime) reportTarget(ctx context.Context, q pgxQuerier, kind, id string) error {
	var exists bool
	var err error
	switch kind {
	case "message":
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat.chat_messages WHERE id=$1)`, id).Scan(&exists)
	case "comment":
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat.interaction_comments WHERE id=$1)`, id).Scan(&exists)
	case "profile", "token":
		exists = addressRE.MatchString(id)
		if !exists {
			return errInvalid
		}
	case "stream":
		_, e := uuid.Parse(id)
		if e != nil {
			return errInvalid
		}
		exists = true
	default:
		return errInvalid
	}
	if err != nil {
		return err
	}
	if !exists {
		return errNotFound
	}
	return nil
}

func (rt *Runtime) insertReport(ctx context.Context, wallet, key, kind, target, reason, reasonText, evidence string) (v2.Report, error) {
	hash := digest(kind, target, reasonText, evidence)
	tx, err := rt.pool.Begin(ctx)
	if err != nil {
		return v2.Report{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "report:"+wallet+":"+key); err != nil {
		return v2.Report{}, err
	}
	var out v2.Report
	var prior string
	err = tx.QueryRow(ctx, `SELECT report_id::text,reporter,target_type,target_id,reason,state,request_hash FROM chat.v2_reports WHERE reporter=$1 AND idempotency_key=$2`, wallet, key).Scan(&out.ReportId, &out.ReporterWallet, &out.TargetType, &out.TargetId, &out.Reason, &out.State, &prior)
	if err == nil {
		if prior != hash {
			return v2.Report{}, errIdempotency
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return v2.Report{}, err
	}
	if err = rt.reportTarget(ctx, tx, kind, target); err != nil {
		return v2.Report{}, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO chat.v2_reports(report_id,reporter,idempotency_key,request_hash,target_type,target_id,reason,reason_text,evidence) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 RETURNING report_id::text,reporter,target_type,target_id,reason,state`, uuid.New(), wallet, key, hash, kind, target, reason, reasonText, evidence).Scan(&out.ReportId, &out.ReporterWallet, &out.TargetType, &out.TargetId, &out.Reason, &out.State)
	if err != nil {
		return v2.Report{}, err
	}
	return out, tx.Commit(ctx)
}
