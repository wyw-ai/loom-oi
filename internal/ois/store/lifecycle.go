package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

// storeLifecycle fences PostgreSQL shutdown before releasing its session
// lock. Admission and Add are atomic with respect to stop, so no operation
// can start between the drain and the acknowledged unlock.
type storeLifecycle struct {
	mu       sync.Mutex
	closing  bool
	stopping chan struct{}
	active   sync.WaitGroup
}

func newStoreLifecycle() *storeLifecycle {
	return &storeLifecycle{stopping: make(chan struct{})}
}

// A nil lifecycle preserves the SQLite path and test-created sqlCore values.
func (l *storeLifecycle) enter() (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closing {
		return nil, ErrUnavailable
	}
	l.active.Add(1)
	return l.active.Done, nil
}

func (l *storeLifecycle) stop() {
	l.mu.Lock()
	if !l.closing {
		l.closing = true
		close(l.stopping)
	}
	l.mu.Unlock()
	// Never hold pgLock.mu while waiting: an admitted Health/write may still
	// need it to finish. Every operation releases its lease after its SQL ends.
	l.active.Wait()
}

// beginStoreTx joins database/sql's background rollback before its caller
// releases its lifecycle lease (or destroys a failed PostgreSQL constructor).
// Tx.Rollback alone is insufficient: awaitDone marks the Tx done before the
// driver's rollback/connection teardown finishes. An explicit Conn.Close waits
// for the transaction's connection lease to be released. SQLite keeps its
// existing DB.BeginTx path.
func beginStoreTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, join bool) (*sql.Tx, func(), error) {
	if !join {
		tx, err := db.BeginTx(ctx, opts)
		if err != nil {
			return nil, nil, err
		}
		return tx, func() { _ = tx.Rollback() }, nil
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx, err := conn.BeginTx(ctx, opts)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return tx, func() {
		_ = tx.Rollback()
		_ = conn.Close()
	}, nil
}

// CleanupError reports a failure to close an offline operation's store.
// Completed only means that the data operation returned success; it does not
// claim the instance lock was released or make replay safe.
type CleanupError struct {
	Operation string
	Completed bool
	Err       error
}

func (e *CleanupError) Error() string {
	if e.Completed {
		return fmt.Sprintf("%s operation completed but cleanup failed: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("%s cleanup failed: %v", e.Operation, e.Err)
}

func (e *CleanupError) Unwrap() error { return e.Err }

func operationCleanupError(operation string, operationErr, closeErr error) error {
	if closeErr == nil {
		return operationErr
	}
	cleanup := &CleanupError{Operation: operation, Completed: operationErr == nil, Err: closeErr}
	if operationErr == nil {
		return cleanup
	}
	return errors.Join(operationErr, cleanup)
}
