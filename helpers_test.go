package projektovemeeting

import (
	"database/sql"
	"testing"
)

func newRepository(t *testing.T) Repository {
	db := newTestDB(t)
	return &RepositorySqlite{DB: db}
}

func newAppRepos(t *testing.T) (*RepositorySqlite, *TxProvider) {
	db := newTestDB(t)
	repo := &RepositorySqlite{DB: db}
	return repo, &TxProvider{DB: db}
}

func newTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite database: %v", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		t.Fatalf("failed to ping sqlite database: %v", err)
	}

	if err := RunMigrations(db); err != nil {
		db.Close()
		t.Fatalf("failed to run migrations: %v", err)
	}
	return db
}
