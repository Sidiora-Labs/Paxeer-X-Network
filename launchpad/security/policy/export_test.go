package policy

import (
	"context"
	"testing"
	"time"
)

func TestAuditExportMissingProviderCannotSucceed(t *testing.T) {
	if _, err := (AuditExporter{}).Export(context.Background(), time.Now().Add(-48*time.Hour)); err == nil {
		t.Fatal("missing retained storage authority reported success")
	}
}
