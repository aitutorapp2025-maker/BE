package database

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Audit-log partitioning. audit_logs is the highest-volume table by far (every
// authenticated request writes a row), so it is RANGE-partitioned by created_at
// into one partition per calendar month (audit_logs_y2026m08, …). Retention
// then becomes a cheap DROP of whole month partitions instead of a multi-million
// row DELETE. Month boundaries are computed in UTC.
//
// Lifecycle:
//   - boot: MigrateAuditPartitions converts a plain audit_logs in place (rename
//     → create partitioned parent → copy → drop old) and pre-creates the
//     current + next month partitions.
//   - writes: AuditLogRepository.RecordBatch self-heals — if an insert hits a
//     month with no partition yet it calls EnsureAuditPartitions and retries,
//     so audit writes never depend on the cleanup cron being enabled.
//   - daily cron: CleanupAuditLogsJob ensures upcoming partitions and drops the
//     ones whose whole month is past the retention cutoff.

// IsAuditPartitioned reports whether audit_logs is a partitioned table.
func IsAuditPartitioned(db *gorm.DB) (bool, error) {
	var yes bool
	err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM pg_partitioned_table pt
		JOIN pg_class c ON c.oid = pt.partrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relname = 'audit_logs')`).Scan(&yes).Error
	return yes, err
}

// monthStartUTC truncates t to the first instant of its UTC calendar month.
func monthStartUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// auditPartitionName is the child-table name for a month (audit_logs_y2026m08).
func auditPartitionName(month time.Time) string {
	return fmt.Sprintf("audit_logs_y%04dm%02d", month.Year(), int(month.Month()))
}

// createAuditPartition creates the child table covering [month, month+1).
// Idempotent (IF NOT EXISTS).
func createAuditPartition(db *gorm.DB, month time.Time) error {
	month = monthStartUTC(month)
	return db.Exec(fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF audit_logs
		 FOR VALUES FROM ('%s 00:00:00+00') TO ('%s 00:00:00+00')`,
		auditPartitionName(month),
		month.Format("2006-01-02"),
		month.AddDate(0, 1, 0).Format("2006-01-02"))).Error
}

// EnsureAuditPartitions makes sure the partitions for the current and next
// month exist, so inserts always have a home even across a month rollover.
func EnsureAuditPartitions(db *gorm.DB, now time.Time) error {
	cur := monthStartUTC(now)
	if err := createAuditPartition(db, cur); err != nil {
		return err
	}
	return createAuditPartition(db, cur.AddDate(0, 1, 0))
}

// MigrateAuditPartitions converts a plain audit_logs table into the monthly
// RANGE-partitioned form, preserving all rows and the id sequence. Idempotent:
// when the table is already partitioned it only ensures upcoming partitions.
// Returns true when a conversion actually ran.
//
// The partitioned parent mirrors the GORM model's columns exactly (so a later
// AutoMigrate sees nothing to change) with one required difference: the primary
// key is (id, created_at), because a partitioned table's PK must include the
// partition key.
func MigrateAuditPartitions(db *gorm.DB) (bool, error) {
	already, err := IsAuditPartitioned(db)
	if err != nil {
		return false, err
	}
	if already {
		return false, EnsureAuditPartitions(db, time.Now())
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`ALTER TABLE audit_logs RENAME TO audit_logs_old`).Error; err != nil {
			return err
		}
		// Reuse the existing id sequence so ids keep counting from where they were.
		if err := tx.Exec(`CREATE TABLE audit_logs (
			id bigint NOT NULL DEFAULT nextval('audit_logs_id_seq'::regclass),
			actor_type varchar(10) NOT NULL,
			actor_id bigint NOT NULL,
			actor_label varchar(150),
			method varchar(8),
			path varchar(200),
			query varchar(400),
			status bigint,
			ip varchar(50),
			user_agent varchar(300),
			latency_ms bigint,
			request_body text,
			response_body text,
			created_at timestamptz NOT NULL DEFAULT now(),
			PRIMARY KEY (id, created_at)
		) PARTITION BY RANGE (created_at)`).Error; err != nil {
			return err
		}

		// Partitions covering every month present in the old data, plus the
		// current and next month.
		var bounds struct{ Min, Max *time.Time }
		if err := tx.Raw(`SELECT min(created_at) AS min, max(created_at) AS max FROM audit_logs_old`).
			Scan(&bounds).Error; err != nil {
			return err
		}
		now := time.Now()
		first := monthStartUTC(now)
		if bounds.Min != nil && bounds.Min.Before(first) {
			first = monthStartUTC(*bounds.Min)
		}
		last := monthStartUTC(now).AddDate(0, 1, 0)
		if bounds.Max != nil && monthStartUTC(*bounds.Max).After(last) {
			last = monthStartUTC(*bounds.Max)
		}
		for m := first; !m.After(last); m = m.AddDate(0, 1, 0) {
			if err := createAuditPartition(tx, m); err != nil {
				return err
			}
		}

		if err := tx.Exec(`INSERT INTO audit_logs
			(id, actor_type, actor_id, actor_label, method, path, query, status,
			 ip, user_agent, latency_ms, request_body, response_body, created_at)
			SELECT id, actor_type, actor_id, actor_label, method, path, query, status,
			 ip, user_agent, latency_ms, request_body, response_body, COALESCE(created_at, now())
			FROM audit_logs_old`).Error; err != nil {
			return err
		}

		// Re-home the sequence before dropping the old table (it is OWNED BY the
		// old table's id column and would be dropped with it otherwise).
		if err := tx.Exec(`ALTER SEQUENCE audit_logs_id_seq OWNED BY audit_logs.id`).Error; err != nil {
			return err
		}
		// Dropping the old table frees the canonical index names for the parent.
		if err := tx.Exec(`DROP TABLE audit_logs_old`).Error; err != nil {
			return err
		}
		for _, idx := range []string{
			`CREATE INDEX idx_audit_logs_created_at ON audit_logs (created_at)`,
			`CREATE INDEX idx_audit_logs_actor_id ON audit_logs (actor_id)`,
			`CREATE INDEX idx_audit_actor_created ON audit_logs (actor_type, created_at DESC)`,
		} {
			if err := tx.Exec(idx).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return err == nil, err
}

// DropOldAuditPartitions drops every monthly partition whose entire range is
// older than cutoff (i.e. upper bound <= cutoff) and returns how many were
// dropped. Only children matching the audit_logs_yYYYYmMM naming are touched.
func DropOldAuditPartitions(db *gorm.DB, cutoff time.Time) (int, error) {
	var names []string
	if err := db.Raw(`SELECT c.relname
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		JOIN pg_class p ON p.oid = i.inhparent
		JOIN pg_namespace n ON n.oid = p.relnamespace
		WHERE n.nspname = current_schema() AND p.relname = 'audit_logs'`).
		Scan(&names).Error; err != nil {
		return 0, err
	}
	dropped := 0
	for _, name := range names {
		var y, m int
		if _, err := fmt.Sscanf(name, "audit_logs_y%dm%d", &y, &m); err != nil {
			continue // not one of ours — leave it alone
		}
		upper := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		if upper.After(cutoff.UTC()) {
			continue
		}
		if err := db.Exec(`DROP TABLE IF EXISTS ` + name).Error; err != nil {
			return dropped, err
		}
		dropped++
	}
	return dropped, nil
}

// DeleteAuditOlderThan is the row-by-row retention fallback for a database
// where the partition conversion hasn't run (or failed at boot).
func DeleteAuditOlderThan(db *gorm.DB, cutoff time.Time) (int64, error) {
	res := db.Exec(`DELETE FROM audit_logs WHERE created_at < ?`, cutoff)
	return res.RowsAffected, res.Error
}
