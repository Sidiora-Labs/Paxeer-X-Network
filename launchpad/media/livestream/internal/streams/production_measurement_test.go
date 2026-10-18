package streams

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestPhase4MediaProductionMeasurement(t *testing.T) {
	path, sha := os.Getenv("L40_MEASUREMENT_MANIFEST"), os.Getenv("L40_MEASUREMENT_MANIFEST_SHA256")
	if path == "" || sha == "" {
		t.Fatal("exact approved media measurement manifest and SHA256 are required")
	}
	cfg, err := LoadMediaMeasurement(path, sha)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	summary, err := RunMediaMeasurement(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CapacityQualified || summary.Scope != "isolated_component" || summary.FinalGate != "9.1" || summary.PlaybackSessions != 20000 || summary.CDNBytes <= 0 || summary.AppVideoBytes != nil {
		t.Fatal("invalid component evidence boundary")
	}
	if summary.OriginP95NS > 150000000 || summary.OriginP99NS > 400000000 || summary.Failures != 0 {
		t.Fatal("actual origin envelope failed")
	}
	t.Logf("media measurement run=%s source=%s artifacts=%v", summary.RunID, summary.Candidate, summary.Artifacts)
}

func TestPhase4MeasurementPercentilesUseNearestRank(t *testing.T) {
	actual := []int64{400, 100, 300, 200}
	if mediaPercentile(actual, .95) != 400 || mediaPercentile(actual, .99) != 400 || mediaPercentile(actual, .5) != 200 {
		t.Fatal("nearest-rank percentile disagrees with raw samples")
	}
	if mediaPercentile(nil, .95) != 0 {
		t.Fatal("empty sample mathematical identity changed")
	}
}

func TestPhase4MediaRetainsActualDependencyRecoveryPaths(t *testing.T) {
	t.Run("rollback", TestConnectedTransactionRollbackDoesNotConsumeAdmission)
	t.Run("provider_database_interruption", TestActualProviderCreationSurvivesCredentialPersistenceInterruption)
	t.Run("observation_outbox_interruption", TestInboxAndObservationRollbackTogetherAtOutboxFailure)
	t.Run("unsigned_started", TestUnsignedStartedCannotEnterInboxOrChangePersistedState)
	t.Run("terminal_replay", TestDelayedStartAfterTerminalObservationRecordsDisagreementOnce)
	t.Run("concurrent_admission", TestConcurrentAdmissionAtMostThree)
	t.Run("terminal_retention", TestExpiredIdempotencyRetirementPreservesOperationAndAudit)
	t.Run("viewer_recovery", TestActualRedisViewerDedupThrottleRecoveryAndQuota)
	t.Run("private_policy", TestPrivatePlaybackPolicyInvalidatesEarlierViewerTokens)
	t.Run("stopped_allocation", TestInterruptedProvisionBindingPreservesStopIntent)
}
