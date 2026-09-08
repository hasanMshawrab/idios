package store

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"
)

// TxObserver learns the duration and outcome of every transaction.
type TxObserver interface {
	ObserveTx(d time.Duration, err error)
}

// errTxPanic is reported to the observer when fn panics: the named err is
// still nil at that point, and the observer must see the panic as a
// failure or the error count lies about a transaction that wrote nothing.
var errTxPanic = errors.New("transaction panicked")

// Writer owns the single write connection; every mutation goes through it.
// Observer, when set, is told about each transaction. Set it before the
// first Tx call: it is read under the write mutex but written without one.
type Writer struct {
	db       *sql.DB
	mu       sync.Mutex
	Observer TxObserver
}

// Tx runs fn in one transaction. Commit on nil, roll back on error. A panic
// in fn rolls back and is re-raised so the handler boundary can log it.
func (w *Writer) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// time.Since, not the clock: a duration is never stored, and the
	// monotonic reading is what a percentile needs.
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			if w.Observer != nil {
				w.Observer.ObserveTx(time.Since(start), errTxPanic)
			}
			panic(r)
		}
		if w.Observer != nil {
			w.Observer.ObserveTx(time.Since(start), err)
		}
	}()

	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
