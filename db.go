package projektovemeeting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type DBExecutor interface {
	// ExecContext executes a query without returning any rows. The args are for any placeholder parameters in the query.
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	// PrepareContext creates a prepared statement for later queries or executions. Multiple queries or executions may be run concurrently from the returned statement. The caller must call the statement's *Stmt.Close method when the statement is no longer needed.
	// The provided context is used for the preparation of the statement, not for the execution of the statement.
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	// QueryContext executes a query that returns rows, typically a SELECT. The args are for any placeholder parameters in the query.
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// QueryRowContext executes a query that is expected to return at most one row. QueryRowContext always returns a non-nil value. Errors are deferred until Row's Scan method is called. If the query selects no rows, the *Row.Scan will return ErrNoRows. Otherwise, *Row.Scan scans the first selected row and discards the rest.
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func RunInTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}

	err = fn(tx)
	if err == nil {
		return tx.Commit()
	}

	rollbackErr := tx.Rollback()
	if rollbackErr != nil {
		return errors.Join(err, rollbackErr)
	}

	return err
}

type TxProvider struct {
	db *sql.DB
}

type RepositorySqlite struct {
	db DBExecutor
}

func (txp TxProvider) Transact(tf func(db Repository) error) error {
	return RunInTx(txp.db, func(tx *sql.Tx) error {
		return tf(&RepositorySqlite{db: tx})
	})
}

const migration = `
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL,
    is_admin INTEGER NOT NULL CHECK (is_admin IN (0, 1)),
    projektove_token TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    token TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS contexts (
    id INTEGER PRIMARY KEY,
    name TEXT,
    context TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    prompt TEXT NOT NULL,
    result TEXT,
    error TEXT,
    context_id INTEGER NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (context_id) REFERENCES contexts(id)
);

CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY,
    projects TEXT NOT NULL,
    fetched_at TIMESTAMP NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS issue_batches (
    id INTEGER PRIMARY KEY,
    file_content BLOB,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS issues (
    id INTEGER PRIMARY KEY,
    subject TEXT NOT NULL,
    description TEXT NOT NULL,
    project_id INTEGER NOT NULL,
    start_date TIMESTAMP NOT NULL,
    due_date TIMESTAMP,
    assigned_to_id INTEGER NOT NULL,
    status TEXT CHECK (status IN ('created', 'submitted', 'submit_failed')),
    projektove_id INTEGER,
    parent TEXT NOT NULL CHECK (parent IN ('prompt', 'batch')),
    parent_id INTEGER NOT NULL
);
`

// NewRepository connects to (or creates) the SQLite database and executes the migration.
func NewRepository(path string) (*TxProvider, Repository, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if _, err := db.Exec(migration); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	repo := &RepositorySqlite{db: db}
	txp := &TxProvider{db: db}

	return txp, repo, nil
}

func (r *RepositorySqlite) StorePrompt(ctx context.Context, user User, obj PromptCreate) (int, error) {
	var errStr string
	if obj.Error != nil {
		errStr = obj.Error.Error()
	}

	query := `INSERT INTO prompts (prompt, result, error, context_id, user_id) VALUES (?, ?, ?, ?, ?)`
	result, execErr := r.db.ExecContext(ctx, query, obj.Prompt, obj.Result, errStr, obj.ContextID, user.ID)
	if execErr != nil {
		return 0, fmt.Errorf("failed to store prompt: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), nil
}

func (r *RepositorySqlite) ListPrompts(ctx context.Context, user User) ([]Prompt, error) {
	prompts := []Prompt{}

	rows, err := r.db.QueryContext(ctx, `
	SELECT
	p.id, p.prompt, p.result, p.error, c.id, c.name, c.context
	FROM prompts p
	JOIN contexts c ON p.context_id = c.id
	WHERE p.user_id = ?
	`, user.ID)
	if err != nil {
		return nil, fmt.Errorf("when listing prompts")
	}

	defer rows.Close()

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

func (r *RepositorySqlite) UpdateProjectsCache(ctx context.Context, user User, projects []ProjektoveProject) error {
	data, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("failed to marshal projects cache: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, execErr := r.db.ExecContext(ctx, "INSERT INTO projects (user_id, projects, fetched_at) VALUES (?, ?, ?)", user.ID, string(data), now)
	if execErr != nil {
		return fmt.Errorf("failed to update projects cache: %w", execErr)
	}

	return nil
}

func (r *RepositorySqlite) ListProjects(ctx context.Context, user User) (ProjectsCacheEntry, error) {
	var rawProjects string
	var fetchedAt time.Time
	err := r.db.QueryRowContext(ctx, "SELECT projects, fetched_at FROM projects WHERE user_id = ? ORDER BY id desc LIMIT 1", user.ID).Scan(&rawProjects, &fetchedAt)
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

func (r *RepositorySqlite) ListContexts(ctx context.Context, user User) ([]LLMContext, error) {
	contexts := []LLMContext{}

	rows, err := r.db.QueryContext(ctx, "SELECT id, name, context FROM contexts WHERE user_id = ?")
	if err != nil {
		return nil, fmt.Errorf("when listing contexts")
	}
	defer rows.Close()

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

func (r *RepositorySqlite) StoreContext(ctx context.Context, user User, c LLMContextCreate) (int, error) {
	result, execErr := r.db.ExecContext(ctx, "INSERT INTO contexts (context, name, user_id) VALUES (?, ?, ?)", c.Context, c.Name, user.ID)
	if execErr != nil {
		return 0, fmt.Errorf("failed to insert context: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), nil
}

func (r *RepositorySqlite) GetContext(ctx context.Context, user User, id int) (LLMContext, error) {
	c := LLMContext{}
	if err := r.db.QueryRowContext(ctx, "SELECT id, name, context from contexts WHERE id = ? AND user_id = ?", id, user.ID).Scan(&c.ID, &c.Name, &c.Context); err != nil {
		return LLMContext{}, fmt.Errorf("when fetching context: %w", err)
	}

	return c, nil
}

func (r *RepositorySqlite) GetUser(ctx context.Context, email string) (User, error) {
	u := User{Email: email, LLMModels: make([]LLMModel, 0)}
	if err := r.db.QueryRowContext(ctx, "SELECT id, projektove_token, is_admin FROM users WHERE email = ?", email).Scan(&u.ID, &u.ProjektoveToken, &u.IsAdmin); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("when fetching user: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, "SELECT provider, model, token FROM providers WHERE user_id = ?", u.ID)

	if err != nil {
		return User{}, fmt.Errorf("when fetching providers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		m := LLMModel{}
		if err := rows.Scan(&m.Provider, &m.Model, &m.Token); err != nil {
			return User{}, fmt.Errorf("when scanning results: %w", err)
		}

		u.LLMModels = append(u.LLMModels, m)
	}

	if err := rows.Err(); err != nil {
		return User{}, fmt.Errorf("when iterating over results: %w", err)
	}

	return u, nil
}

func (r *RepositorySqlite) StoreUser(ctx context.Context, obj UserCreate) (int, error) {
	result, execErr := r.db.ExecContext(ctx, "INSERT INTO users (email, is_admin, projektove_token) VALUES (?, false, ?)", obj.Email, obj.ProjektoveToken)
	if execErr != nil {
		return 0, fmt.Errorf("failed to insert user: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	if len(obj.Models) > 0 {
		var sb strings.Builder
		sb.WriteString("INSERT INTO providers (user_id, provider, model, token) VALUES ")
		args := make([]any, 0, len(obj.Models)*4)
		for i, m := range obj.Models {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("(?, ?, ?, ?)")
			args = append(args, id, m.Provider, m.Model, m.Token)
		}
		if _, err := r.db.ExecContext(ctx, sb.String(), args...); err != nil {
			return 0, fmt.Errorf("failed to insert providers: %w", err)
		}
	}

	return int(id), nil
}

func (r *RepositorySqlite) UpdateUser(ctx context.Context, u User, obj UserUpdate) error {
	result, execErr := r.db.ExecContext(ctx, "UPDATE users SET projektove_token = ? WHERE id = ?", obj.ProjektoveToken, u.ID)
	if execErr != nil {
		return fmt.Errorf("failed to update user: %w", execErr)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("user with id %d not found", u.ID)
	}

	if _, err := r.db.ExecContext(ctx, "DELETE FROM providers WHERE user_id = ?", u.ID); err != nil {
		return fmt.Errorf("failed to delete providers: %w", err)
	}

	if len(obj.Models) > 0 {
		var sb strings.Builder
		sb.WriteString("INSERT INTO providers (user_id, provider, model, token) VALUES ")
		args := make([]any, 0, len(obj.Models)*4)
		for i, m := range obj.Models {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("(?, ?, ?, ?)")
			args = append(args, u.ID, m.Provider, m.Model, m.Token)
		}
		if _, err := r.db.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("failed to insert providers: %w", err)
		}
	}

	return nil
}

func (r *RepositorySqlite) StoreIssue(ctx context.Context, user User, issue IssueCreate) (int, error) {
	query := `INSERT INTO issues (parent, parent_id, subject, description, project_id, start_date, due_date, assigned_to_id, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, execErr := r.db.ExecContext(ctx, query, issue.Parent, issue.ParentID, issue.Subject, issue.Description, issue.ProjectID, issue.StartDate, issue.DueDate, issue.AssignedToID, IssueStatusCreated)
	if execErr != nil {
		return 0, fmt.Errorf("failed to store issue: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), nil
}

func (r *RepositorySqlite) UpdateIssue(ctx context.Context, user User, id int, obj IssueUpdate) error {
	query := `UPDATE issues SET status = ?, subject = ?, description = ?, project_id = ?, start_date = ?, due_date = ?, assigned_to_id = ?, projektove_id = ? WHERE id = ? AND user_id = ?`
	result, execErr := r.db.ExecContext(ctx, query, obj.Status, obj.Subject, obj.Description, obj.ProjectID, obj.StartDate, obj.DueDate, obj.AssignedToID, obj.ProjektoveID, id, user.ID)
	if execErr != nil {
		return fmt.Errorf("failed to update issue: %w", execErr)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("issue with id %d not found", id)
	}

	return nil
}

func (r *RepositorySqlite) ListIssues(ctx context.Context, user User, parent IssueParent, parentID int) ([]Issue, error) {
	issues := []Issue{}

	rows, err := r.db.QueryContext(ctx, "SELECT id, subject, description, project_id, start_date, due_date, assigned_to_id, projektove_id FROM issues WHERE parent = ? AND parent_id = ? AND user_id = ?", parent, parentID, user.ID)
	if err != nil {
		return nil, fmt.Errorf("when listing issues: %w", err)
	}

	for rows.Next() {
		i := Issue{}
		if err := rows.Scan(&i.ID, &i.Parent, &i.ParentID, &i.Subject, &i.Description, &i.ProjectID, &i.StartDate, &i.DueDate, &i.AssignedToID, &i.ProjektoveID); err != nil {
			return nil, fmt.Errorf("when scanning results: %w", err)
		}
		issues = append(issues, i)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over results: %w", err)
	}

	return issues, nil
}
