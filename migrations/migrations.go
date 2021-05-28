package migrations

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/http"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	"github.com/golang-migrate/migrate/v4/source/httpfs"
)

//go:embed *.sql
var migrations embed.FS

type embedFSDriver struct {
	httpfs.PartialDriver
}

func init() {
	source.Register("embed", &embedFSDriver{})
}

func (d *embedFSDriver) Open(rawURL string) (source.Driver, error) {
	err := d.PartialDriver.Init(http.FS(migrations), ".")
	if err != nil {
		return nil, err
	}

	return d, nil
}

func Migrate(dsn string) error {

	sqlDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open DB: %w", err)
	}

	d, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("create driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"embed://", "postgres", d)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}

	err = m.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate: %w", err)
	}

	return nil
}
