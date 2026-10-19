package s3

import (
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/aws/smithy-go"
)

type mockAPIError struct {
	code    string
	message string
}

func (e *mockAPIError) Error() string                 { return e.message }
func (e *mockAPIError) ErrorCode() string             { return e.code }
func (e *mockAPIError) ErrorMessage() string          { return e.message }
func (e *mockAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

func TestClassifyS3Error_NoSuchBucket(t *testing.T) {
	err := &mockAPIError{code: "NoSuchBucket", message: "bucket not found"}
	te := ClassifyS3Error("snapshot-restore", "my-bucket", "key", "us-east-1", err)

	if !strings.Contains(te.Message, "my-bucket") {
		t.Errorf("expected bucket in message, got: %s", te.Message)
	}
	if !strings.Contains(te.Hint, "SEI_SNAPSHOT_BUCKET") {
		t.Errorf("expected env var hint, got: %s", te.Hint)
	}
	if te.Retryable {
		t.Error("NoSuchBucket should not be retryable")
	}
	if te.Task != "snapshot-restore" {
		t.Errorf("Task = %q, want snapshot-restore", te.Task)
	}
}

func TestClassifyS3Error_NoSuchKey(t *testing.T) {
	err := &mockAPIError{code: "NoSuchKey", message: "not found"}
	te := ClassifyS3Error("configure-genesis", "bucket", "genesis.json", "eu-central-1", err)

	if !strings.Contains(te.Message, "genesis.json") {
		t.Errorf("expected key in message, got: %s", te.Message)
	}
	if te.Retryable {
		t.Error("NoSuchKey should not be retryable")
	}
}

func TestClassifyS3Error_AccessDenied(t *testing.T) {
	err := &mockAPIError{code: "AccessDenied", message: "forbidden"}
	te := ClassifyS3Error("snapshot-restore", "bucket", "key", "us-east-1", err)

	if !strings.Contains(te.Hint, "IAM") {
		t.Errorf("expected IAM hint, got: %s", te.Hint)
	}
	if te.Retryable {
		t.Error("AccessDenied should not be retryable")
	}
}

func TestClassifyS3Error_SlowDown(t *testing.T) {
	err := &mockAPIError{code: "SlowDown", message: "throttled"}
	te := ClassifyS3Error("snapshot-restore", "bucket", "key", "us-east-1", err)

	if !te.Retryable {
		t.Error("SlowDown should be retryable")
	}
}

func TestClassifyS3Error_NetworkError(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}
	te := ClassifyS3Error("snapshot-restore", "bucket", "key", "us-east-1", err)

	if !te.Retryable {
		t.Error("network error should be retryable")
	}
	if !strings.Contains(te.Hint, "VPC") {
		t.Errorf("expected VPC hint, got: %s", te.Hint)
	}
}

func TestClassifyS3Error_UnknownError(t *testing.T) {
	err := fmt.Errorf("something unexpected")
	te := ClassifyS3Error("export-state", "bucket", "key", "us-east-1", err)

	if te.Retryable {
		t.Error("unknown error should not be retryable")
	}
	if te.Cause != "something unexpected" {
		t.Errorf("Cause = %q, want original error", te.Cause)
	}
}

func TestClassifyS3Error_ErrorString(t *testing.T) {
	err := &mockAPIError{code: "NoSuchBucket", message: "bucket not found"}
	te := ClassifyS3Error("snapshot-restore", "my-bucket", "key", "us-east-1", err)

	s := te.Error()
	if !strings.Contains(s, "snapshot-restore") {
		t.Errorf("Error() should contain task name, got: %s", s)
	}
	if !strings.Contains(s, "[hint:") {
		t.Errorf("Error() should contain hint, got: %s", s)
	}
}

func TestClassifyS3Error_UnknownAPICode(t *testing.T) {
	err := &mockAPIError{code: "InternalError", message: "server error"}
	te := ClassifyS3Error("snapshot-restore", "bucket", "key", "us-east-1", err)

	if !strings.Contains(te.Message, "InternalError") {
		t.Errorf("expected error code in message, got: %s", te.Message)
	}
	if te.Retryable {
		t.Error("unknown API error should not be retryable by default")
	}
}
