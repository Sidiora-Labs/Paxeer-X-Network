package cliutil

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EmitStatus writes err as a metav1.Status so callers can `jq -r
// .reason` to discriminate failure classes. Wraps non-Status errors
// as InternalError.
func EmitStatus(w io.Writer, err error) {
	if err == nil {
		return
	}
	status := ToStatus(err)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(status)
}

// ToStatus extracts an apiserver metav1.Status from err (walking %w
// wraps), or synthesizes an InternalError for a plain Go error.
func ToStatus(err error) *metav1.Status {
	var apiErr apierrors.APIStatus
	if errors.As(err, &apiErr) {
		s := apiErr.Status()
		return &s
	}
	return &metav1.Status{
		TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
		Status:   metav1.StatusFailure,
		Reason:   metav1.StatusReasonInternalError,
		Message:  err.Error(),
		Code:     http.StatusInternalServerError,
	}
}

// UsageError reports CLI validation failures as Status{Reason:Invalid}
// (BadRequest) so the stderr discriminator above still works.
func UsageError(format string, args ...interface{}) error {
	return apierrors.NewBadRequest(fmt.Sprintf(format, args...))
}
