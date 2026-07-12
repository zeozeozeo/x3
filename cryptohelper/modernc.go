package cryptohelper

import (
	"context"
	"database/sql"

	"modernc.org/sqlite"
)

func init() {
	d := &sqlite.Driver{}
	d.RegisterConnectionHook(func(conn sqlite.ExecQuerierContext, dsn string) error {
		for _, pragma := range []string{
			"PRAGMA foreign_keys = ON",
			"PRAGMA journal_mode = WAL",
			"PRAGMA synchronous = NORMAL",
			"PRAGMA busy_timeout = 5000",
		} {
			if _, err := conn.ExecContext(context.Background(), pragma, nil); err != nil {
				return err
			}
		}
		return nil
	})
	sql.Register("sqlite3-fk-wal", d)
}
