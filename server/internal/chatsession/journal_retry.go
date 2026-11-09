package chatsession

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"syscall"

	"github.com/go-sql-driver/mysql"
	"github.com/mattn/go-sqlite3"
)

const journalTransientAttempts = 6

// Retry only transport failures and explicit retryable SQL/storage responses.
// Unknown errors, invalid content, revoked owners and permission failures stay
// terminal. Every retry still runs the original owner/publication fences.
func transientJournalError(err error) bool {
	if errors.Is(err, ErrQueueOwnerLost) || errors.Is(err, ErrQueueRetired) || errors.Is(err, ErrJournalGap) || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, driver.ErrBadConn) || errors.Is(err, sql.ErrConnDone) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return true
	}
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) {
		status := response.HTTPStatusCode()
		return status == 408 || status == 429 || status >= 500 && status <= 599
	}
	var mysqlError *mysql.MySQLError
	if errors.As(err, &mysqlError) {
		switch mysqlError.Number {
		case 1040, 1053, 1205, 1213, 2006, 2013:
			return true
		}
	}
	var sqliteError sqlite3.Error
	return errors.As(err, &sqliteError) && (sqliteError.Code == sqlite3.ErrBusy || sqliteError.Code == sqlite3.ErrLocked)
}
