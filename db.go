package projektovemeeting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DBSqlite struct {
	db *sql.DB
}

const migration = `
CREATE TABLE IF NOT EXISTS prompts (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	prompt TEXT,
	result TEXT,
	error TEXT
);

CREATE TABLE IF NOT EXISTS projects (
	id INTEGER PRIMARY KEY,
	projects TEXT,
	fetched_at TIMESTAMP
);
`

// NewDB connects to (or creates) the SQLite database and executes the migration.
func NewDB(path string) (DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if _, err := db.Exec(migration); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &DBSqlite{db: db}, nil
}

func (s *DBSqlite) StorePrompt(ctx context.Context, prompt string, result string, err error) error {
	var errStr string
	if err != nil {
		errStr = err.Error()
	}

	query := `INSERT INTO prompts (prompt, result, error) VALUES (?, ?, ?)`
	_, execErr := s.db.ExecContext(ctx, query, prompt, result, errStr)
	if execErr != nil {
		return fmt.Errorf("failed to store prompt: %w", execErr)
	}

	return nil
}

func (s *DBSqlite) UpdateProjectsCache(ctx context.Context, projects []ProjektoveProject) error {
	data, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("failed to marshal projects cache: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	query := "INSERT INTO projects (projects, fetched_at) VALUES (?, ?)"

	_, execErr := s.db.ExecContext(ctx, query, string(data), now)
	if execErr != nil {
		return fmt.Errorf("failed to update projects cache: %w", execErr)
	}

	return nil
}

var ErrNoProjectsFound = errors.New("no projects found")

func (s *DBSqlite) ListProjects(ctx context.Context) (ProjectsCacheEntry, error) {
	query := `SELECT projects, fetched_at FROM projects ORDER BY id desc LIMIT 1`

	var rawProjects string
	var fetchedAt time.Time
	err := s.db.QueryRowContext(ctx, query).Scan(&rawProjects, &fetchedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return ProjectsCacheEntry{}, ErrNoProjectsFound
		}
		return ProjectsCacheEntry{}, fmt.Errorf("failed to query projects cache: %w", err)
	}

	entry := ProjectsCacheEntry{FetchedAt: fetchedAt}
	if err := json.Unmarshal([]byte(rawProjects), &entry.Projects); err != nil {
		return ProjectsCacheEntry{}, fmt.Errorf("failed to unmarshal projects cache: %w", err)
	}

	return entry, nil
}
