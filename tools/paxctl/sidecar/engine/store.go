package engine

// ResultStore persists task results across all lifecycle states.
// Implementations must be safe for concurrent use.
type ResultStore interface {
	// Save persists a TaskResult. If a result with the same ID already
	// exists, it is overwritten (upsert).
	Save(r *TaskResult) error

	// Get returns a result by ID, or (nil, nil) when not found.
	Get(id string) (*TaskResult, error)

	// List returns the most recent results, newest first, up to limit.
	List(limit int) ([]TaskResult, error)

	// ListStaleTasks returns tasks left in "running" state from a
	// previous process that exited without completing them.
	ListStaleTasks() ([]TaskResult, error)

	// Delete removes a result by ID. Returns true if it existed.
	Delete(id string) (bool, error)

	// DeleteByType removes all results of the given task type and returns
	// how many rows were removed. Used by mark-not-ready to purge recorded
	// mark-ready results so a stranded running one cannot rehydrate and
	// release a node hold after a data wipe.
	DeleteByType(taskType string) (int, error)

	// LatestByType returns the most recently submitted result of the given
	// task type (any status), or (nil, nil) when none exists. Rehydration
	// uses it to detect a hold that supersedes a stranded mark-ready even
	// after the hold's own record has gone terminal (e.g. a failed purge
	// persisted it Failed), which a stale-only scan would miss.
	LatestByType(taskType string) (*TaskResult, error)

	// Ping verifies the store is responsive. Used by liveness checks.
	Ping() error

	// Close releases underlying resources.
	Close() error
}
