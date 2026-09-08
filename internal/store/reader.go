package store

import "database/sql"

// Reader is the read-only pool on the same file.
type Reader struct {
	db *sql.DB
}

// DB returns the underlying read-only connection pool.
func (r *Reader) DB() *sql.DB { return r.db }
