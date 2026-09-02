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
	error TEXT,
	context_id INT,
	FOREIGN KEY (context_id) REFERENCES contexts(id)
);

CREATE TABLE IF NOT EXISTS projects (
	id INTEGER PRIMARY KEY,
	projects TEXT,
	fetched_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS contexts (
	id INTEGER PRIMARY KEY,
	name TEXT,
	context TEXT
)
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

func (s *DBSqlite) StorePrompt(ctx context.Context, obj PromptCreate) error {
	var errStr string
	if obj.Error != nil {
		errStr = obj.Error.Error()
	}

	query := `INSERT INTO prompts (prompt, result, error, context_id) VALUES (?, ?, ?, ?)`
	_, execErr := s.db.ExecContext(ctx, query, obj.Prompt, obj.Result, errStr, obj.ContextID)
	if execErr != nil {
		return fmt.Errorf("failed to store prompt: %w", execErr)
	}

	return nil
}

func (s *DBSqlite) ListPrompts(ctx context.Context) ([]Prompt, error) {
	prompts := []Prompt{}

	rows, err := s.db.QueryContext(ctx, "SELECT p.id, p.prompt, p.result, p.error, c.id, c.name, c.context FROM prompts p join contexts c on p.context_id = c.id")
	if err != nil {
		return nil, fmt.Errorf("when listing prompts")
	}

	for rows.Next() {
		p := Prompt{}
		if err := rows.Scan(&p.ID, &p.Prompt, &p.Result, &p.Error, &p.Context.ID, &p.Context.Name, &p.Context.Context); err != nil {
			return nil, fmt.Errorf("when scanning results: %w", err)
		}
		prompts = append(prompts, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over results: %w", err)
	}

	return prompts, nil
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

func (s *DBSqlite) ListContexts(ctx context.Context) ([]LLMContext, error) {
	contexts := []LLMContext{}

	rows, err := s.db.QueryContext(ctx, "SELECT id, name, context FROM contexts")
	if err != nil {
		return nil, fmt.Errorf("when listing contexts")
	}

	for rows.Next() {
		c := LLMContext{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Context); err != nil {
			return nil, fmt.Errorf("when scanning results: %w", err)
		}
		contexts = append(contexts, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over results: %w", err)
	}

	return contexts, nil
}

func (s *DBSqlite) AddContext(ctx context.Context, c LLMContextCreate) error {
	_, execErr := s.db.ExecContext(ctx, "INSERT INTO contexts (context, name) VALUES (?, ?)", c.Context, c.Name)
	if execErr != nil {
		return fmt.Errorf("failed to insert context: %w", execErr)
	}

	return nil
}

func (s *DBSqlite) GetContext(ctx context.Context, id int) (LLMContext, error) {
	c := LLMContext{}
	if err := s.db.QueryRowContext(ctx, "SELECT id, name, context from contexts WHERE id = ?", id).Scan(&c.ID, &c.Name, &c.Context); err != nil {
		return LLMContext{}, fmt.Errorf("when fetching context: %w", err)
	}

	return c, nil
}
