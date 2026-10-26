// Package migration is tooling-only; applications do not run migrations on startup.
package migration

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"time"

	driver "github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	mysql "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Open requires agreement between the explicit target and the secret DSN.
// The caller must close the returned migrator, which owns the SQL connection.
func Open(dsn, database, directory string) (*migrate.Migrate, error) {
	if dsn == "" || database == "" || directory == "" {
		return nil, errors.New("DSN, explicit database and migration directory are required")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		return nil, errors.New("invalid MySQL DSN (value omitted)")
	}
	if cfg.DBName != database {
		return nil, errors.New("DSN database does not match explicit target")
	}
	cfg.MultiStatements = true
	cfg.Timeout = 10 * time.Second
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	databaseDriver, err := mysql.WithInstance(db, &mysql.Config{DatabaseName: database})
	if err != nil {
		db.Close()
		return nil, err
	}
	abs, err := filepath.Abs(directory)
	if err != nil {
		databaseDriver.Close()
		return nil, err
	}
	source := (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
	m, err := migrate.NewWithDatabaseInstance(source, database, databaseDriver)
	if err != nil {
		databaseDriver.Close()
		return nil, err
	}
	return m, nil
}
