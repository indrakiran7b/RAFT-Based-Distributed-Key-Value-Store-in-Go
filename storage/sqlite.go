package storage

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

func InitDB(path string) *sql.DB {
	// Single connection: SQLite is single-writer, extra connections just queue.
	// Keeping one idle connection avoids reconnect overhead entirely.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		log.Fatal("Failed to open DB:", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // keep it alive forever

	pragmas := []string{
		// WAL mode: readers don't block writers and vice versa.
		"PRAGMA journal_mode=WAL;",

		// Wait up to 5 s instead of returning SQLITE_BUSY immediately.
		// Needed even with a single connection pool — the Go sql package can
		// briefly have two connections racing during pool warm-up.
		"PRAGMA busy_timeout=5000;",

		// NORMAL sync: flushes WAL on checkpoint, not on every transaction commit.
		// Safe against OS crash; only vulnerable to power loss (acceptable for a
		// Raft node — the log is replicated across the cluster anyway).
		"PRAGMA synchronous=NORMAL;",

		// 64 MB page cache in memory — reduces disk reads for hot keys.
		"PRAGMA cache_size=-65536;",

		// Store temp tables and indices in memory.
		"PRAGMA temp_store=MEMORY;",

		// Memory-map 256 MB of the database file — fast reads via OS page cache.
		"PRAGMA mmap_size=268435456;",

		// Allow WAL to grow to 1000 pages before auto-checkpoint.
		// Fewer checkpoints = fewer stalls during write bursts.
		"PRAGMA wal_autocheckpoint=1000;",
	}

	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			log.Printf("Warning: pragma failed (%s): %v", p, err)
		}
	}

	// Main key-value table
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS kv (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		);`); err != nil {
		log.Fatal("Failed to create kv table:", err)
	}

	// Persisted idempotency table — survives node restarts, unlike the old
	// in-memory map. The UNIQUE constraint means INSERT OR IGNORE is the
	// dedup check, which is fast and atomic within the batch transaction.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS idempotency (
			ikey TEXT PRIMARY KEY
		);`); err != nil {
		log.Fatal("Failed to create idempotency table:", err)
	}

	return db
}
