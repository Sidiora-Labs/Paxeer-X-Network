package engine

import "database/sql"

// migrate runs pending schema migrations. Each version is wrapped in an
// explicit transaction so that DDL and the user_version bump are atomic.
func migrate(db *sql.DB) error {
	var version int
	_ = db.QueryRow("PRAGMA user_version").Scan(&version)

	if version < 1 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS task_results (
				id           TEXT PRIMARY KEY,
				type         TEXT    NOT NULL,
				status       TEXT    NOT NULL,
				params       TEXT,
				schedule     TEXT,
				error        TEXT    NOT NULL DEFAULT '',
				submitted_at TEXT    NOT NULL,
				completed_at TEXT,
				next_run_at  TEXT
			);
			CREATE INDEX IF NOT EXISTS idx_task_results_submitted_at
				ON task_results (submitted_at DESC);
			CREATE INDEX IF NOT EXISTS idx_task_results_schedule
				ON task_results (next_run_at) WHERE schedule IS NOT NULL;
		`); err != nil {
			return err
		}

		if _, err := tx.Exec("PRAGMA user_version = 1"); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	if version < 2 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
			DROP INDEX IF EXISTS idx_task_results_schedule;
			ALTER TABLE task_results DROP COLUMN schedule;
			ALTER TABLE task_results DROP COLUMN next_run_at;
		`); err != nil {
			return err
		}

		if _, err := tx.Exec("PRAGMA user_version = 2"); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	if version < 3 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.Exec(`
			ALTER TABLE task_results ADD COLUMN run INTEGER NOT NULL DEFAULT 1;
		`); err != nil {
			return err
		}

		if _, err := tx.Exec("PRAGMA user_version = 3"); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	if version < 4 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// result holds a handler's structured output as raw JSON; NULL
		// for the common case of a handler that emits no result.
		if _, err := tx.Exec(`
			ALTER TABLE task_results ADD COLUMN result TEXT;
		`); err != nil {
			return err
		}

		if _, err := tx.Exec("PRAGMA user_version = 4"); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	if version < 5 {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		// tx_markers: pre-broadcast signed-tx bytes so a crashed sign-tx task
		// re-adopts the identical tx instead of re-signing. Engine-owned. A
		// pre-v5 downgrade leaves markers unread (the guard degrades).
		if _, err := tx.Exec(`
			CREATE TABLE IF NOT EXISTS tx_markers (
				task_id        TEXT PRIMARY KEY,
				tx_hash        TEXT    NOT NULL,
				tx_bytes       BLOB    NOT NULL,
				account_number INTEGER NOT NULL,
				sequence       INTEGER NOT NULL,
				chain_id       TEXT    NOT NULL,
				created_at     TEXT    NOT NULL
			);
		`); err != nil {
			return err
		}

		if _, err := tx.Exec("PRAGMA user_version = 5"); err != nil {
			return err
		}

		if err := tx.Commit(); err != nil {
			return err
		}
	}

	return nil
}
