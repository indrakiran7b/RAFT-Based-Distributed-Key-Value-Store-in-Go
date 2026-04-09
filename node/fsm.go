package node

import (
	"database/sql"
	"encoding/json"
	"io"
	"sync"

	"raft-kv/model"

	"github.com/hashicorp/raft"
)

type FSM struct {
	kv map[string]string
	mu sync.RWMutex
	db *sql.DB
}

func NewFSM(db *sql.DB) *FSM {
	f := &FSM{
		kv: make(map[string]string),
		db: db,
	}
	f.loadFromDB()
	return f
}

func (f *FSM) loadFromDB() {
	rows, err := f.db.Query("SELECT key, value FROM kv")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			f.kv[k] = v
		}
	}
}

func (f *FSM) Apply(log *raft.Log) interface{} {
	results, _ := f.applyBatch([]*raft.Log{log})
	if len(results) > 0 {
		return results[0]
	}
	return nil
}

func (f *FSM) ApplyBatch(logs []*raft.Log) []interface{} {
	results, _ := f.applyBatch(logs)
	return results
}

func (f *FSM) applyBatch(logs []*raft.Log) ([]interface{}, error) {
	results := make([]interface{}, len(logs))

	type entry struct {
		idx int
		cmd model.Command
	}
	entries := make([]entry, 0, len(logs))
	for i, l := range logs {
		if l.Type != raft.LogCommand {
			continue
		}
		var cmd model.Command
		if err := json.Unmarshal(l.Data, &cmd); err != nil {
			continue
		}
		entries = append(entries, entry{i, cmd})
	}

	if len(entries) == 0 {
		return results, nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	tx, err := f.db.Begin()
	if err != nil {
		return results, err
	}

	kvStmt, err := tx.Prepare("INSERT OR REPLACE INTO kv(key,value) VALUES (?,?)")
	if err != nil {
		tx.Rollback()
		return results, err
	}
	defer kvStmt.Close()

	idempStmt, err := tx.Prepare("INSERT OR IGNORE INTO idempotency(ikey) VALUES (?)")
	if err != nil {
		tx.Rollback()
		return results, err
	}
	defer idempStmt.Close()

	for _, e := range entries {
		cmd := e.cmd

		if cmd.IdempotencyKey != "" {
			res, err := idempStmt.Exec(cmd.IdempotencyKey)
			if err != nil {
				continue
			}
			affected, _ := res.RowsAffected()
			if affected == 0 {
				// duplicate — skip
				continue
			}
		}

		if cmd.Op == "PUT" {
			kvStmt.Exec(cmd.Key, cmd.Value)
			f.kv[cmd.Key] = cmd.Value
		}
	}

	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return results, err
	}

	return results, nil
}

func (f *FSM) Get(key string) string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.kv[key]
}

// ── Snapshot / Restore ────────────────────────────────────────────────────────
// Raft calls Snapshot() periodically based on SnapshotThreshold (set in node.go).
// After a snapshot is taken, Raft truncates the log — this prevents BoltDB from
// growing unboundedly. Snapshot includes idempotency keys so dedup survives restore.

type fsmSnapshot struct {
	KV         map[string]string `json:"kv"`
	Idempotent []string          `json:"idempotent"`
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Deep copy kv
	kvCopy := make(map[string]string, len(f.kv))
	for k, v := range f.kv {
		kvCopy[k] = v
	}

	// Read idempotency keys from SQLite
	rows, err := f.db.Query("SELECT ikey FROM idempotency")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ikeys []string
	for rows.Next() {
		var ikey string
		if rows.Scan(&ikey) == nil {
			ikeys = append(ikeys, ikey)
		}
	}

	return &fsmSnapshot{KV: kvCopy, Idempotent: ikeys}, nil
}

func (s *fsmSnapshot) Persist(sink raft.SnapshotSink) error {
	if err := json.NewEncoder(sink).Encode(s); err != nil {
		sink.Cancel()
		return err
	}
	return sink.Close()
}

func (s *fsmSnapshot) Release() {}

// Restore replaces all FSM state from a snapshot.
// Called on crash recovery or when a new follower catches up via snapshot.
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	var snap fsmSnapshot
	if err := json.NewDecoder(rc).Decode(&snap); err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	tx, err := f.db.Begin()
	if err != nil {
		return err
	}

	// Wipe both tables
	tx.Exec("DELETE FROM kv")
	tx.Exec("DELETE FROM idempotency")

	// Restore kv
	kvStmt, err := tx.Prepare("INSERT INTO kv(key,value) VALUES(?,?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer kvStmt.Close()
	for k, v := range snap.KV {
		kvStmt.Exec(k, v)
	}

	// Restore idempotency keys
	idempStmt, err := tx.Prepare("INSERT INTO idempotency(ikey) VALUES(?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer idempStmt.Close()
	for _, ikey := range snap.Idempotent {
		idempStmt.Exec(ikey)
	}

	if err := tx.Commit(); err != nil {
		tx.Rollback()
		return err
	}

	f.kv = snap.KV
	return nil
}
