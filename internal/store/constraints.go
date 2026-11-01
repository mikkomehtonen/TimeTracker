package store

import (
	"errors"

	sqlite "modernc.org/sqlite"
)

// Extended SQLITE_CONSTRAINT codes for the unique-style constraints this
// app relies on. NOT NULL, CHECK, and FOREIGN KEY failures deliberately do
// not match: callers label these results "already exists" or "conflict", so
// misreporting an FK or CHECK failure as a duplicate would be wrong.
const (
	sqliteConstraintUnique = 2067 // SQLITE_CONSTRAINT_UNIQUE
	// PRIMARY KEY collisions are unreachable today because every insert lets
	// SQLite assign the rowid; the code stays as defense for future inserts.
	sqliteConstraintPrimaryKey = 1555 // SQLITE_CONSTRAINT_PRIMARYKEY
)

// sqliteBusyClass is the SQLITE_BUSY class (masked before the extended code).
const sqliteBusyClass = 5

// IsConstraintViolation reports whether err is a UNIQUE (including partial
// index) or PRIMARY KEY violation.
func IsConstraintViolation(err error) bool {
	var e *sqlite.Error
	if errors.As(err, &e) {
		switch e.Code() {
		case sqliteConstraintUnique, sqliteConstraintPrimaryKey:
			return true
		}
	}
	return false
}

// IsBusy reports the retryable SQLITE_BUSY class (including the extended
// SQLITE_BUSY_SNAPSHOT a WAL writer gets when its read snapshot goes stale).
func IsBusy(err error) bool {
	var e *sqlite.Error
	if errors.As(err, &e) {
		return e.Code()&0xff == sqliteBusyClass
	}
	return false
}
