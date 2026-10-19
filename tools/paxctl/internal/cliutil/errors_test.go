package cliutil

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// T11 — error→Status mapping. NotFound/Invalid/Internal all surface as a
// metav1.Status on stderr with the matching .reason.

func TestEmitStatus_NilNoop(t *testing.T) {
	var buf bytes.Buffer
	EmitStatus(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("nil error wrote %d bytes; want 0", buf.Len())
	}
}

func TestEmitStatus_PreservesAPIStatus(t *testing.T) {
	in := apierrors.NewNotFound(schema.GroupResource{Group: "sei.io", Resource: "seinodes"}, "missing")
	var buf bytes.Buffer
	EmitStatus(&buf, in)

	var got metav1.Status
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Reason != metav1.StatusReasonNotFound {
		t.Errorf("reason = %q; want NotFound", got.Reason)
	}
	if got.Code != 404 {
		t.Errorf("code = %d; want 404", got.Code)
	}
	if got.Status != metav1.StatusFailure {
		t.Errorf("status = %q; want Failure", got.Status)
	}
}

func TestEmitStatus_UnwrapsThroughFmtErrorf(t *testing.T) {
	inner := apierrors.NewNotFound(schema.GroupResource{Group: "sei.io", Resource: "seinodes"}, "missing")
	wrapped := fmt.Errorf("apply SeiNode default/missing: %w", inner)

	var buf bytes.Buffer
	EmitStatus(&buf, wrapped)

	var got metav1.Status
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Reason != metav1.StatusReasonNotFound {
		t.Errorf("reason = %q; want NotFound (errors.As must walk %%w wrap)", got.Reason)
	}
	if got.Code != 404 {
		t.Errorf("code = %d; want 404", got.Code)
	}
}

func TestEmitStatus_InvalidReason(t *testing.T) {
	in := apierrors.NewInvalid(schema.GroupKind{Group: "sei.io", Kind: "SeiNetwork"}, "net", nil)
	var buf bytes.Buffer
	EmitStatus(&buf, in)
	var got metav1.Status
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Reason != metav1.StatusReasonInvalid {
		t.Errorf("reason = %q; want Invalid (admission-immutability surfaces this)", got.Reason)
	}
}

func TestEmitStatus_GenericErrorWrappedAsInternal(t *testing.T) {
	var buf bytes.Buffer
	EmitStatus(&buf, errors.New("boom"))

	var got metav1.Status
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Reason != metav1.StatusReasonInternalError {
		t.Errorf("reason = %q; want InternalError", got.Reason)
	}
	if got.Code != 500 {
		t.Errorf("code = %d; want 500", got.Code)
	}
	if got.Message != "boom" {
		t.Errorf("message = %q; want boom", got.Message)
	}
}

func TestUsageError_RoundTripsThroughEmit(t *testing.T) {
	var buf bytes.Buffer
	EmitStatus(&buf, UsageError("bad flag: %s", "--preset"))

	var got metav1.Status
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Reason != metav1.StatusReasonBadRequest {
		t.Errorf("reason = %q; want BadRequest", got.Reason)
	}
	if got.Code != 400 {
		t.Errorf("code = %d; want 400", got.Code)
	}
}
