// Package operationlog records what wtff did, as an append only stream of
// structured events.
//
// The log is written for two readers with different needs. A person asking what
// happened to a file needs the record to be complete and in order. A future
// undo or history command needs it to be machine readable without guessing.
// One JSON object per line satisfies both and survives partial writes: a
// truncated final line is discardable without invalidating what came before.
//
// Logging never blocks an operation. A failure to write a record is reported
// through the writer's error state, not returned to the caller mid deletion,
// because refusing to delete because a log line could not be written trades a
// real problem for a bookkeeping one. Callers that need to know may ask.
package operationlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Kind classifies an event. The set is deliberately small: a reader should be
// able to filter the log without a lookup table.
type Kind string

const (
	// KindPlanned records that an entry was included in a plan.
	KindPlanned Kind = "planned"
	// KindStaged records a reversible removal into the staging area.
	KindStaged Kind = "staged"
	// KindPurged records an irreversible removal.
	KindPurged Kind = "purged"
	// KindSkipped records an entry that was in a plan but not acted on, which
	// is the most important kind for diagnosing surprises.
	KindSkipped Kind = "skipped"
	// KindRestored records an entry returned from staging by undo.
	KindRestored Kind = "restored"
	// KindFailed records an entry whose operation was attempted and errored.
	KindFailed Kind = "failed"
	// KindSession records the start or end of a command run.
	KindSession Kind = "session"
)

// Event is one line of the log.
type Event struct {
	Time      time.Time `json:"time"`
	Command   string    `json:"command"`
	Kind      Kind      `json:"kind"`
	Path      string    `json:"path,omitempty"`
	Outcome   string    `json:"outcome,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Bytes     int64     `json:"bytes,omitempty"`
	BatchID   string    `json:"batch_id,omitempty"`
	SizeKnown bool      `json:"size_known"`
}

// Writer appends events to a log file.
type Writer struct {
	mu      sync.Mutex
	file    *os.File
	command string
	lastErr error
	closed  bool
}

// DefaultPath returns the conventional log location for the invoking user.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "wtff", "operations.log"), nil
}

// Open prepares a writer at the given path, creating the directory if needed.
//
// The log is opened append only. Two wtff runs writing at once is expected, and
// append mode with one write per record keeps their lines interleaved rather
// than overlapping.
func Open(path, command string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("cannot create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open log: %w", err)
	}
	return &Writer{file: file, command: command}, nil
}

// Discard returns a writer that accepts events and drops them, for tests and
// for runs where logging is deliberately disabled.
func Discard() *Writer { return &Writer{} }

// Record appends an event. The command and timestamp are filled in when absent.
func (w *Writer) Record(event Event) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || w.closed {
		return
	}
	if event.Time.IsZero() {
		event.Time = time.Now().UTC()
	}
	if event.Command == "" {
		event.Command = w.command
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		w.lastErr = err
		return
	}
	if _, err := w.file.Write(append(encoded, '\n')); err != nil {
		w.lastErr = err
	}
}

// Err reports the first write failure observed, if any. Callers that care
// whether the audit trail is complete should check this at the end of a run.
func (w *Writer) Err() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastErr
}

// Close flushes and releases the log file.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil || w.closed {
		return nil
	}
	w.closed = true
	return w.file.Close()
}
