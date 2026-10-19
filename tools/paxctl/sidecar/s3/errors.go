package s3

import (
	"errors"
	"fmt"
	"net"

	"github.com/aws/smithy-go"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

// ClassifyS3Error wraps an S3 error with operator-actionable context.
// The returned TaskError includes the task name, S3 coordinates, a
// human-readable message, and a hint for resolution.
func ClassifyS3Error(task, bucket, key, region string, err error) *engine.TaskError {
	te := &engine.TaskError{
		Task:      task,
		Operation: "S3",
		Cause:     err.Error(),
	}

	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchBucket":
			te.Message = fmt.Sprintf("bucket %q does not exist in region %s", bucket, region)
			te.Hint = "verify SEI_SNAPSHOT_BUCKET and SEI_SNAPSHOT_REGION environment variables"
		case "NoSuchKey":
			te.Message = fmt.Sprintf("object %q not found in s3://%s", key, bucket)
			te.Hint = "the snapshot or genesis file may not have been uploaded yet"
		case "AccessDenied":
			te.Message = fmt.Sprintf("access denied to s3://%s/%s in region %s", bucket, key, region)
			te.Hint = "check the pod's ServiceAccount IAM role has s3:GetObject permission on this bucket"
		case "SlowDown", "ServiceUnavailable":
			te.Message = fmt.Sprintf("S3 throttled request to s3://%s/%s", bucket, key)
			te.Hint = "transient S3 throttling; the task will be retried"
			te.Retryable = true
		default:
			te.Message = fmt.Sprintf("S3 error %s on s3://%s/%s", apiErr.ErrorCode(), bucket, key)
		}
		return te
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		te.Message = fmt.Sprintf("network error accessing s3://%s/%s", bucket, key)
		te.Hint = "check VPC endpoints and DNS resolution for S3"
		te.Retryable = true
		return te
	}

	te.Message = fmt.Sprintf("unexpected error accessing s3://%s/%s: %v", bucket, key, err)
	return te
}
