package projektovemeeting

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	sqliteMigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
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
	DB *sql.DB
}

type RepositorySqlite struct {
	DB DBExecutor
}

func (txp TxProvider) Transact(tf func(db Repository) error) error {
	return RunInTx(txp.DB, func(tx *sql.Tx) error {
		return tf(&RepositorySqlite{DB: tx})
	})
}

//go:embed migrations
var migrations embed.FS

func RunMigrations(db *sql.DB) error {
	d, err := iofs.New(migrations, "migrations/sqlite")
	if err != nil {
		return fmt.Errorf("when creating iofs driver: %w", err)
	}

	driver, err := sqliteMigrate.WithInstance(db, &sqliteMigrate.Config{})
	if err != nil {
		return fmt.Errorf("when constructing sqlite instance: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", d, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("when constructing migration: %w", err)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return fmt.Errorf("when running migration: %w", err)
	}

	return nil
}

// NewRepository connects to (or creates) the SQLite database and executes the migration.
func NewRepository(path string) (*TxProvider, Repository, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	if err := RunMigrations(db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	repo := &RepositorySqlite{DB: db}
	txp := &TxProvider{DB: db}

	return txp, repo, nil
}

func (r *RepositorySqlite) StorePrompt(ctx context.Context, org UserProjektoveOrganization, obj PromptCreate) (int, string, error) {
	var errStr string
	if obj.Error != nil {
		errStr = obj.Error.Error()
	}

	if obj.Status == "" {
		obj.Status = PromptStatusDone
	}

	if obj.CreatedAt.IsZero() {
		obj.CreatedAt = CurrentTime()
	}

	uuid := newUUID()

	query := `INSERT INTO prompts (uuid, prompt, result, error, context_id, status, model_id, file_content, created_at, user_projektove_organization_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, execErr := r.DB.ExecContext(ctx, query, uuid, obj.Prompt, obj.Result, errStr, obj.ContextID, obj.Status, obj.ModelID, obj.FileContent, obj.CreatedAt, org.ID)
	if execErr != nil {
		return 0, "", fmt.Errorf("failed to store prompt: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), uuid, nil
}

const promptSelect = `
SELECT p.id, p.uuid, p.prompt, p.result, p.error, p.status, p.model_id, p.file_content, p.created_at, p.user_projektove_organization_id,
c.id, c.uuid, c.name, c.context,
m.model, pr.provider
FROM prompts p
JOIN contexts c ON p.context_id = c.id
JOIN models m ON p.model_id = m.id
JOIN providers pr ON m.provider_id = pr.id`

func (r *RepositorySqlite) ListPrompts(ctx context.Context, user User, limit, offset int) ([]Prompt, bool, error) {
	prompts := []Prompt{}

	rows, err := r.DB.QueryContext(ctx, `
	SELECT
	p.id, p.uuid, p.prompt, p.result, p.error, p.status, p.model_id, p.file_content, p.created_at, p.user_projektove_organization_id,
	c.id, c.uuid, c.name, c.context,
	m.model, pr.provider,
	(SELECT COUNT(*) FROM issues i WHERE i.parent = 'prompt' AND i.parent_id = p.id) AS total_issues,
	(SELECT COUNT(*) FROM issues i WHERE i.parent = 'prompt' AND i.parent_id = p.id AND i.status = 'submitted') AS submitted_issues
	FROM prompts p
	JOIN contexts c ON p.context_id = c.id
	JOIN models m ON p.model_id = m.id
	JOIN providers pr ON m.provider_id = pr.id
	WHERE p.user_projektove_organization_id IN (SELECT id FROM users_projektove_organizations WHERE user_id = ?)
	ORDER BY p.id DESC
	LIMIT ? OFFSET ?
	`, user.ID, limit+1, offset)
	if err != nil {
		return nil, false, fmt.Errorf("when listing prompts: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		p := Prompt{}
		if err := rows.Scan(&p.ID, &p.UUID, &p.Prompt, &p.Result, &p.Error, &p.Status, &p.ModelID, &p.FileContent, &p.CreatedAt, &p.UserProjektoveOrganizationID, &p.Context.ID, &p.Context.UUID, &p.Context.Name, &p.Context.Context, &p.Model, &p.Provider, &p.TotalIssues, &p.SubmittedIssues); err != nil {
			return nil, false, fmt.Errorf("when scanning results: %w", err)
		}
		prompts = append(prompts, p)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("when iterating over results: %w", err)
	}

	hasMore := false
	if len(prompts) > limit {
		hasMore = true
		prompts = prompts[:limit]
	}

	return prompts, hasMore, nil
}

func (r *RepositorySqlite) GetPrompt(ctx context.Context, org UserProjektoveOrganization, id int) (Prompt, error) {
	p := Prompt{}
	row := r.DB.QueryRowContext(ctx, promptSelect+`
	WHERE p.id = ? AND p.user_projektove_organization_id = ?
	`, id, org.ID)
	if err := scanPrompt(row, &p); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Prompt{}, ErrPromptNotFound
		}
		return Prompt{}, fmt.Errorf("when getting prompt: %w", err)
	}

	return p, nil
}

func (r *RepositorySqlite) GetPromptByUUID(ctx context.Context, org UserProjektoveOrganization, uuid string) (Prompt, error) {
	p := Prompt{}
	row := r.DB.QueryRowContext(ctx, promptSelect+`
	WHERE p.uuid = ? AND p.user_projektove_organization_id = ?
	`, uuid, org.ID)
	if err := scanPrompt(row, &p); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Prompt{}, ErrPromptNotFound
		}
		return Prompt{}, fmt.Errorf("when getting prompt: %w", err)
	}

	return p, nil
}

func scanPrompt(row *sql.Row, p *Prompt) error {
	return row.Scan(&p.ID, &p.UUID, &p.Prompt, &p.Result, &p.Error, &p.Status, &p.ModelID, &p.FileContent, &p.CreatedAt, &p.UserProjektoveOrganizationID, &p.Context.ID, &p.Context.UUID, &p.Context.Name, &p.Context.Context, &p.Model, &p.Provider)
}

func (r *RepositorySqlite) SetPromptProcessing(ctx context.Context, org UserProjektoveOrganization, id int, prompt string) error {
	result, err := r.DB.ExecContext(ctx, `UPDATE prompts SET status = ?, prompt = ? WHERE id = ? AND user_projektove_organization_id = ?`, PromptStatusProcessing, prompt, id, org.ID)
	if err != nil {
		return fmt.Errorf("failed to mark prompt as processing: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrPromptNotFound
	}

	return nil
}

func (r *RepositorySqlite) CompletePrompt(ctx context.Context, org UserProjektoveOrganization, id int, obj PromptComplete) error {
	result, err := r.DB.ExecContext(ctx, `UPDATE prompts SET prompt = ?, result = ?, error = ?, status = ? WHERE id = ? AND user_projektove_organization_id = ?`, obj.Prompt, obj.Result, obj.Error, obj.Status, id, org.ID)
	if err != nil {
		return fmt.Errorf("failed to complete prompt: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrPromptNotFound
	}

	return nil
}

func (r *RepositorySqlite) StoreBatch(ctx context.Context, org UserProjektoveOrganization, fileContent string) (int, string, error) {
	uuid := newUUID()

	result, execErr := r.DB.ExecContext(ctx, "INSERT INTO issue_batches (uuid, file_content, user_projektove_organization_id) VALUES (?, ?, ?)", uuid, fileContent, org.ID)
	if execErr != nil {
		return 0, "", fmt.Errorf("failed to store batch: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), uuid, nil
}

const batchSelect = `
SELECT b.id, b.uuid, b.file_content, b.created_at, b.user_projektove_organization_id
FROM issue_batches b`

func (r *RepositorySqlite) ListBatches(ctx context.Context, user User, limit, offset int) ([]Batch, bool, error) {
	batches := []Batch{}

	rows, err := r.DB.QueryContext(ctx, `
	SELECT
	b.id, b.uuid, b.file_content, b.created_at, b.user_projektove_organization_id,
	(SELECT COUNT(*) FROM issues i WHERE i.parent = 'batch' AND i.parent_id = b.id) AS total_issues,
	(SELECT COUNT(*) FROM issues i WHERE i.parent = 'batch' AND i.parent_id = b.id AND i.status = 'submitted') AS submitted_issues
	FROM issue_batches b
	WHERE b.user_projektove_organization_id IN (SELECT id FROM users_projektove_organizations WHERE user_id = ?)
	ORDER BY b.id DESC
	LIMIT ? OFFSET ?
	`, user.ID, limit+1, offset)
	if err != nil {
		return nil, false, fmt.Errorf("when listing batches: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		b := Batch{}
		if err := rows.Scan(&b.ID, &b.UUID, &b.FileContent, &b.CreatedAt, &b.UserProjektoveOrganizationID, &b.TotalIssues, &b.SubmittedIssues); err != nil {
			return nil, false, fmt.Errorf("when scanning results: %w", err)
		}
		batches = append(batches, b)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("when iterating over results: %w", err)
	}

	hasMore := false
	if len(batches) > limit {
		hasMore = true
		batches = batches[:limit]
	}

	return batches, hasMore, nil
}

func (r *RepositorySqlite) GetBatch(ctx context.Context, org UserProjektoveOrganization, id int) (Batch, error) {
	b := Batch{}
	err := r.DB.QueryRowContext(ctx, batchSelect+`
	WHERE b.id = ? AND b.user_projektove_organization_id = ?
	`, id, org.ID).Scan(&b.ID, &b.UUID, &b.FileContent, &b.CreatedAt, &b.UserProjektoveOrganizationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Batch{}, ErrBatchNotFound
		}
		return Batch{}, fmt.Errorf("when getting batch: %w", err)
	}

	return b, nil
}

func (r *RepositorySqlite) GetBatchByUUID(ctx context.Context, org UserProjektoveOrganization, uuid string) (Batch, error) {
	b := Batch{}
	err := r.DB.QueryRowContext(ctx, batchSelect+`
	WHERE b.uuid = ? AND b.user_projektove_organization_id = ?
	`, uuid, org.ID).Scan(&b.ID, &b.UUID, &b.FileContent, &b.CreatedAt, &b.UserProjektoveOrganizationID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Batch{}, ErrBatchNotFound
		}
		return Batch{}, fmt.Errorf("when getting batch: %w", err)
	}

	return b, nil
}

func (r *RepositorySqlite) EnqueueTask(ctx context.Context, obj TaskCreate) (int, error) {
	payload, err := json.Marshal(obj.Payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal task payload: %w", err)
	}

	result, err := r.DB.ExecContext(ctx, `INSERT INTO tasks (type, payload, status) VALUES (?, ?, ?)`, obj.Type, string(payload), TaskStatusPending)
	if err != nil {
		return 0, fmt.Errorf("failed to enqueue task: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), nil
}

func (r *RepositorySqlite) ClaimTask(ctx context.Context) (Task, bool, error) {
	var t Task
	var typ string
	var payload []byte

	err := r.DB.QueryRowContext(ctx, `SELECT id, type, payload FROM tasks WHERE status = ? ORDER BY id LIMIT 1`, TaskStatusPending).Scan(&t.ID, &typ, &payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Task{}, false, nil
		}
		return Task{}, false, fmt.Errorf("when finding pending task: %w", err)
	}

	result, err := r.DB.ExecContext(ctx, `UPDATE tasks SET status = ?, started_at = ? WHERE id = ? AND status = ?`, TaskStatusProcessing, time.Now().UTC(), t.ID, TaskStatusPending)
	if err != nil {
		return Task{}, false, fmt.Errorf("when claiming task: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Task{}, false, fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return Task{}, false, nil
	}

	t.Type = TaskType(typ)
	if err := json.Unmarshal(payload, &t.Payload); err != nil {
		return Task{}, false, fmt.Errorf("failed to unmarshal task payload: %w", err)
	}
	t.Status = TaskStatusProcessing

	return t, true, nil
}

func (r *RepositorySqlite) CompleteTask(ctx context.Context, id int, status TaskStatus, errMsg string) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE tasks SET status = ?, error = ?, finished_at = ? WHERE id = ?`, status, errMsg, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	return nil
}

func (r *RepositorySqlite) ResetOrphanedTasks(ctx context.Context) error {
	_, err := r.DB.ExecContext(ctx, `UPDATE tasks SET status = ?, error = NULL, started_at = NULL, finished_at = NULL WHERE status = ?`, TaskStatusPending, TaskStatusProcessing)
	if err != nil {
		return fmt.Errorf("failed to reset orphaned tasks: %w", err)
	}

	return nil
}

func (r *RepositorySqlite) UpdateProjectsCache(ctx context.Context, org UserProjektoveOrganization, projects []ProjektoveProject) error {
	data, err := json.Marshal(projects)
	if err != nil {
		return fmt.Errorf("failed to marshal projects cache: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)

	_, execErr := r.DB.ExecContext(ctx, "INSERT INTO projects (user_projektove_organization_id, projects, fetched_at) VALUES (?, ?, ?)", org.ID, string(data), now)
	if execErr != nil {
		return fmt.Errorf("failed to update projects cache: %w", execErr)
	}

	return nil
}

func (r *RepositorySqlite) ListProjects(ctx context.Context, org UserProjektoveOrganization) (ProjectsCacheEntry, error) {
	var rawProjects string
	var fetchedAt time.Time
	err := r.DB.QueryRowContext(ctx, "SELECT projects, fetched_at FROM projects WHERE user_projektove_organization_id = ? ORDER BY id desc LIMIT 1", org.ID).Scan(&rawProjects, &fetchedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return ProjectsCacheEntry{}, ErrNoProjectsFound
		}
		return ProjectsCacheEntry{}, fmt.Errorf("failed to query projects cache: %w", err)
	}

	entry := ProjectsCacheEntry{FetchedAt: fetchedAt, UserProjektoveOrgID: org.ID}
	if err := json.Unmarshal([]byte(rawProjects), &entry.Projects); err != nil {
		return ProjectsCacheEntry{}, fmt.Errorf("failed to unmarshal projects cache: %w", err)
	}

	return entry, nil
}

func (r *RepositorySqlite) ListContexts(ctx context.Context, user User) ([]LLMContext, error) {
	contexts := []LLMContext{}

	rows, err := r.DB.QueryContext(ctx, "SELECT id, uuid, name, context FROM contexts WHERE user_id = ?", user.ID)
	if err != nil {
		return nil, fmt.Errorf("when listing contexts")
	}
	defer rows.Close()

	for rows.Next() {
		c := LLMContext{}
		if err := rows.Scan(&c.ID, &c.UUID, &c.Name, &c.Context); err != nil {
			return nil, fmt.Errorf("when scanning results: %w", err)
		}
		contexts = append(contexts, c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over results: %w", err)
	}

	return contexts, nil
}

func (r *RepositorySqlite) StoreContext(ctx context.Context, user User, c LLMContextCreate) (int, string, error) {
	uuid := newUUID()

	result, execErr := r.DB.ExecContext(ctx, "INSERT INTO contexts (uuid, context, name, user_id) VALUES (?, ?, ?, ?)", uuid, c.Context, c.Name, user.ID)
	if execErr != nil {
		return 0, "", fmt.Errorf("failed to insert context: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), uuid, nil
}

const contextCols = "id, uuid, name, context"

func (r *RepositorySqlite) GetContext(ctx context.Context, user User, id int) (LLMContext, error) {
	c := LLMContext{}
	if err := r.DB.QueryRowContext(ctx, "SELECT "+contextCols+" from contexts WHERE id = ? AND user_id = ?", id, user.ID).Scan(&c.ID, &c.UUID, &c.Name, &c.Context); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LLMContext{}, ErrContextNotFound
		}
		return LLMContext{}, fmt.Errorf("when fetching context: %w", err)
	}

	return c, nil
}

func (r *RepositorySqlite) GetContextByUUID(ctx context.Context, user User, uuid string) (LLMContext, error) {
	c := LLMContext{}
	if err := r.DB.QueryRowContext(ctx, "SELECT "+contextCols+" from contexts WHERE uuid = ? AND user_id = ?", uuid, user.ID).Scan(&c.ID, &c.UUID, &c.Name, &c.Context); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return LLMContext{}, ErrContextNotFound
		}
		return LLMContext{}, fmt.Errorf("when fetching context: %w", err)
	}

	return c, nil
}

func (r *RepositorySqlite) DeleteContextByUUID(ctx context.Context, user User, uuid string) error {
	c, err := r.GetContextByUUID(ctx, user, uuid)
	if err != nil {
		return err
	}

	var count int
	if err := r.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM prompts WHERE context_id = ?", c.ID).Scan(&count); err != nil {
		return fmt.Errorf("when checking prompt usage of context: %w", err)
	}

	if count > 0 {
		return ErrContextInUse
	}

	if _, err := r.DB.ExecContext(ctx, "DELETE FROM contexts WHERE id = ? AND user_id = ?", c.ID, user.ID); err != nil {
		return fmt.Errorf("when deleting context: %w", err)
	}

	return nil
}

func (r *RepositorySqlite) GetUser(ctx context.Context, email string) (User, error) {
	u := User{Email: email}
	err := r.DB.QueryRowContext(ctx, "SELECT id, password_hash, is_admin FROM users WHERE email = ?", email).Scan(&u.ID, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("when fetching user: %w", err)
	}

	return u, nil
}

func (r *RepositorySqlite) GetUserByID(ctx context.Context, id int) (User, error) {
	u := User{ID: id}
	err := r.DB.QueryRowContext(ctx, "SELECT email, password_hash, is_admin FROM users WHERE id = ?", id).Scan(&u.Email, &u.PasswordHash, &u.IsAdmin)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("when fetching user: %w", err)
	}

	return u, nil
}

func (r *RepositorySqlite) StoreUser(ctx context.Context, obj UserCreate) (int, error) {
	result, execErr := r.DB.ExecContext(ctx, "INSERT INTO users (email, password_hash, is_admin) VALUES (?, ?, ?)", obj.Email, obj.PasswordHash, obj.IsAdmin)
	if execErr != nil {
		return 0, fmt.Errorf("failed to insert user: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), nil
}

func (r *RepositorySqlite) ListProviders(ctx context.Context) ([]Provider, error) {
	providers := []Provider{}

	rows, err := r.DB.QueryContext(ctx, "SELECT id, provider FROM providers ORDER BY provider")
	if err != nil {
		return nil, fmt.Errorf("when listing providers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		p := Provider{}
		if err := rows.Scan(&p.ID, &p.Provider); err != nil {
			return nil, fmt.Errorf("when scanning providers: %w", err)
		}
		providers = append(providers, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over providers: %w", err)
	}

	return providers, nil
}

func (r *RepositorySqlite) GetOrCreateProvider(ctx context.Context, provider string) (int, error) {
	if _, err := r.DB.ExecContext(ctx, "INSERT OR IGNORE INTO providers (provider) VALUES (?)", provider); err != nil {
		return 0, fmt.Errorf("failed to insert provider: %w", err)
	}

	var id int
	if err := r.DB.QueryRowContext(ctx, "SELECT id FROM providers WHERE provider = ?", provider).Scan(&id); err != nil {
		return 0, fmt.Errorf("failed to fetch provider: %w", err)
	}

	return id, nil
}

func (r *RepositorySqlite) GetOrCreateModel(ctx context.Context, providerID int, model string) (int, string, error) {
	uuid := newUUID()

	if _, err := r.DB.ExecContext(ctx, "INSERT OR IGNORE INTO models (uuid, provider_id, model) VALUES (?, ?, ?)", uuid, providerID, model); err != nil {
		return 0, "", fmt.Errorf("failed to insert model: %w", err)
	}

	var id int
	var existingUUID string
	if err := r.DB.QueryRowContext(ctx, "SELECT id, uuid FROM models WHERE provider_id = ? AND model = ?", providerID, model).Scan(&id, &existingUUID); err != nil {
		return 0, "", fmt.Errorf("failed to fetch model: %w", err)
	}

	return id, existingUUID, nil
}

func (r *RepositorySqlite) ListModels(ctx context.Context) ([]Model, error) {
	models := []Model{}

	rows, err := r.DB.QueryContext(ctx, `
	SELECT m.id, m.uuid, m.provider_id, pr.provider, m.model
	FROM models m
	JOIN providers pr ON m.provider_id = pr.id
	ORDER BY pr.provider, m.model`)
	if err != nil {
		return nil, fmt.Errorf("when listing models: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		m := Model{}
		if err := rows.Scan(&m.ID, &m.UUID, &m.ProviderID, &m.Provider, &m.Model); err != nil {
			return nil, fmt.Errorf("when scanning model: %w", err)
		}
		models = append(models, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over models: %w", err)
	}

	return models, nil
}

func (r *RepositorySqlite) ListUserModels(ctx context.Context, userID int) ([]UserModel, error) {
	models := []UserModel{}

	rows, err := r.DB.QueryContext(ctx, `
	SELECT um.id, um.uuid, um.user_id, um.model_id, pr.provider, m.model, um.token
	FROM users_models um
	JOIN models m ON um.model_id = m.id
	JOIN providers pr ON m.provider_id = pr.id
	WHERE um.user_id = ?
	ORDER BY um.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("when listing user models: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		um := UserModel{}
		if err := rows.Scan(&um.ID, &um.UUID, &um.UserID, &um.ModelID, &um.Provider, &um.Model, &um.Token); err != nil {
			return nil, fmt.Errorf("when scanning user model: %w", err)
		}
		models = append(models, um)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over user models: %w", err)
	}

	return models, nil
}

func (r *RepositorySqlite) StoreUserModel(ctx context.Context, userID int, modelID int, token string) (int, string, error) {
	uuid := newUUID()

	result, execErr := r.DB.ExecContext(ctx, "INSERT INTO users_models (uuid, user_id, model_id, token) VALUES (?, ?, ?, ?)", uuid, userID, modelID, token)
	if execErr != nil {
		return 0, "", fmt.Errorf("failed to insert user model: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), uuid, nil
}

const userModelSelect = `
SELECT um.id, um.uuid, um.user_id, um.model_id, pr.provider, m.model, um.token
FROM users_models um
JOIN models m ON um.model_id = m.id
JOIN providers pr ON m.provider_id = pr.id`

func (r *RepositorySqlite) GetUserModelByUUID(ctx context.Context, userID int, uuid string) (UserModel, error) {
	um := UserModel{}
	err := r.DB.QueryRowContext(ctx, userModelSelect+` WHERE um.uuid = ? AND um.user_id = ?`, uuid, userID).Scan(&um.ID, &um.UUID, &um.UserID, &um.ModelID, &um.Provider, &um.Model, &um.Token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserModel{}, ErrUserModelNotFound
		}
		return UserModel{}, fmt.Errorf("when getting user model: %w", err)
	}
	return um, nil
}

func (r *RepositorySqlite) GetUserModelByModelID(ctx context.Context, userID int, modelID int) (UserModel, error) {
	um := UserModel{}
	err := r.DB.QueryRowContext(ctx, userModelSelect+` WHERE um.model_id = ? AND um.user_id = ?`, modelID, userID).Scan(&um.ID, &um.UUID, &um.UserID, &um.ModelID, &um.Provider, &um.Model, &um.Token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserModel{}, ErrUserModelNotFound
		}
		return UserModel{}, fmt.Errorf("when getting user model: %w", err)
	}
	return um, nil
}

func (r *RepositorySqlite) UpdateUserModel(ctx context.Context, userID int, uuid string, token string) error {
	result, err := r.DB.ExecContext(ctx, "UPDATE users_models SET token = ? WHERE uuid = ? AND user_id = ?", token, uuid, userID)
	if err != nil {
		return fmt.Errorf("failed to update user model: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrUserModelNotFound
	}

	return nil
}

func (r *RepositorySqlite) DeleteUserModel(ctx context.Context, userID int, uuid string) error {
	result, err := r.DB.ExecContext(ctx, "DELETE FROM users_models WHERE uuid = ? AND user_id = ?", uuid, userID)
	if err != nil {
		return fmt.Errorf("failed to delete user model: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrUserModelNotFound
	}

	return nil
}

func (r *RepositorySqlite) ListUserProjektoveOrganizations(ctx context.Context, userID int) ([]UserProjektoveOrganization, error) {
	orgs := []UserProjektoveOrganization{}

	rows, err := r.DB.QueryContext(ctx, `
	SELECT uo.id, uo.uuid, uo.user_id, uo.organization_id, uo.token, o.name, o.api_url, o.browser_url
	FROM users_projektove_organizations uo
	JOIN projektove_organizations o ON uo.organization_id = o.id
	WHERE uo.user_id = ?
	ORDER BY uo.id`, userID)
	if err != nil {
		return nil, fmt.Errorf("when listing user projektove organizations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		uo := UserProjektoveOrganization{}
		if err := rows.Scan(&uo.ID, &uo.UUID, &uo.UserID, &uo.OrganizationID, &uo.Token, &uo.OrgName, &uo.APIURL, &uo.BrowserURL); err != nil {
			return nil, fmt.Errorf("when scanning user organization: %w", err)
		}
		orgs = append(orgs, uo)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over user organizations: %w", err)
	}

	return orgs, nil
}

func (r *RepositorySqlite) GetUserProjektoveOrganization(ctx context.Context, uuid string) (UserProjektoveOrganization, error) {
	uo := UserProjektoveOrganization{}
	err := r.DB.QueryRowContext(ctx, `
	SELECT uo.id, uo.uuid, uo.user_id, uo.organization_id, uo.token, o.name, o.api_url, o.browser_url
	FROM users_projektove_organizations uo
	JOIN projektove_organizations o ON uo.organization_id = o.id
	WHERE uo.uuid = ?`, uuid).Scan(&uo.ID, &uo.UUID, &uo.UserID, &uo.OrganizationID, &uo.Token, &uo.OrgName, &uo.APIURL, &uo.BrowserURL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserProjektoveOrganization{}, ErrOrganizationNotFound
		}
		return UserProjektoveOrganization{}, fmt.Errorf("when getting user organization: %w", err)
	}

	return uo, nil
}

func (r *RepositorySqlite) GetUserProjektoveOrganizationByID(ctx context.Context, id int) (UserProjektoveOrganization, error) {
	uo := UserProjektoveOrganization{}
	err := r.DB.QueryRowContext(ctx, `
	SELECT uo.id, uo.uuid, uo.user_id, uo.organization_id, uo.token, o.name, o.api_url, o.browser_url
	FROM users_projektove_organizations uo
	JOIN projektove_organizations o ON uo.organization_id = o.id
	WHERE uo.id = ?`, id).Scan(&uo.ID, &uo.UUID, &uo.UserID, &uo.OrganizationID, &uo.Token, &uo.OrgName, &uo.APIURL, &uo.BrowserURL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return UserProjektoveOrganization{}, ErrOrganizationNotFound
		}
		return UserProjektoveOrganization{}, fmt.Errorf("when getting user organization: %w", err)
	}

	return uo, nil
}

func (r *RepositorySqlite) UpdateUserOrganizationToken(ctx context.Context, userID int, uuid string, token string) error {
	result, err := r.DB.ExecContext(ctx, "UPDATE users_projektove_organizations SET token = ? WHERE uuid = ? AND user_id = ?", token, uuid, userID)
	if err != nil {
		return fmt.Errorf("failed to update user organization token: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrOrganizationNotFound
	}

	return nil
}

func (r *RepositorySqlite) StoreUserProjectoveOrganization(ctx context.Context, userID int, orgID int, token string) (string, error) {
	var existingUUID string
	err := r.DB.QueryRowContext(ctx, "SELECT uuid FROM users_projektove_organizations WHERE user_id = ? AND organization_id = ?", userID, orgID).Scan(&existingUUID)
	if err == nil {
		if _, err := r.DB.ExecContext(ctx, "UPDATE users_projektove_organizations SET token = ? WHERE uuid = ? AND user_id = ?", token, existingUUID, userID); err != nil {
			return "", fmt.Errorf("failed to update existing user organization token: %w", err)
		}
		return existingUUID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("failed to check user organization: %w", err)
	}

	uuid := newUUID()
	if _, err := r.DB.ExecContext(ctx, "INSERT INTO users_projektove_organizations (uuid, user_id, organization_id, token) VALUES (?, ?, ?, ?)", uuid, userID, orgID, token); err != nil {
		return "", fmt.Errorf("failed to insert user organization: %w", err)
	}

	return uuid, nil
}

func (r *RepositorySqlite) ListOrganizationUsers(ctx context.Context, orgID int) ([]ProjektoveOrganizationUser, error) {
	users := []ProjektoveOrganizationUser{}

	rows, err := r.DB.QueryContext(ctx, "SELECT id, uuid, organization_id, name, projektove_id FROM projektove_organizations_users WHERE organization_id = ? ORDER BY name", orgID)
	if err != nil {
		return nil, fmt.Errorf("when listing organization users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		u := ProjektoveOrganizationUser{}
		if err := rows.Scan(&u.ID, &u.UUID, &u.OrganizationID, &u.Name, &u.ProjektoveID); err != nil {
			return nil, fmt.Errorf("when scanning organization user: %w", err)
		}
		users = append(users, u)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over organization users: %w", err)
	}

	return users, nil
}

func (r *RepositorySqlite) ListProjektoveOrganizations(ctx context.Context) ([]ProjektoveOrganization, error) {
	orgs := []ProjektoveOrganization{}

	rows, err := r.DB.QueryContext(ctx, "SELECT id, uuid, name, api_url, browser_url FROM projektove_organizations ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("when listing projektove organizations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		o := ProjektoveOrganization{}
		if err := rows.Scan(&o.ID, &o.UUID, &o.Name, &o.APIURL, &o.BrowserURL); err != nil {
			return nil, fmt.Errorf("when scanning organization: %w", err)
		}
		orgs = append(orgs, o)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over organizations: %w", err)
	}

	return orgs, nil
}

func (r *RepositorySqlite) StoreProjektoveOrganization(ctx context.Context, name, apiURL, browserURL string) (int, string, error) {
	uuid := newUUID()
	result, err := r.DB.ExecContext(ctx, "INSERT INTO projektove_organizations (uuid, name, api_url, browser_url) VALUES (?, ?, ?, ?)", uuid, name, apiURL, browserURL)
	if err != nil {
		return 0, "", fmt.Errorf("failed to store projektove organization: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, "", fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), uuid, nil
}

func (r *RepositorySqlite) GetProjektoveOrganizationByUUID(ctx context.Context, uuid string) (ProjektoveOrganization, error) {
	o := ProjektoveOrganization{}
	err := r.DB.QueryRowContext(ctx, "SELECT id, uuid, name, api_url, browser_url FROM projektove_organizations WHERE uuid = ?", uuid).Scan(&o.ID, &o.UUID, &o.Name, &o.APIURL, &o.BrowserURL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjektoveOrganization{}, ErrOrganizationNotFound
		}
		return ProjektoveOrganization{}, fmt.Errorf("when getting projektove organization: %w", err)
	}

	return o, nil
}

func (r *RepositorySqlite) GetProjektoveOrganizationByName(ctx context.Context, name string) (ProjektoveOrganization, error) {
	o := ProjektoveOrganization{}
	err := r.DB.QueryRowContext(ctx, "SELECT id, uuid, name, api_url, browser_url FROM projektove_organizations WHERE name = ?", name).Scan(&o.ID, &o.UUID, &o.Name, &o.APIURL, &o.BrowserURL)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjektoveOrganization{}, ErrOrganizationNotFound
		}
		return ProjektoveOrganization{}, fmt.Errorf("when getting projektove organization by name: %w", err)
	}

	return o, nil
}

func (r *RepositorySqlite) UpdateProjektoveOrganization(ctx context.Context, uuid string, name, apiURL, browserURL string) error {
	result, err := r.DB.ExecContext(ctx, "UPDATE projektove_organizations SET name = ?, api_url = ?, browser_url = ? WHERE uuid = ?", name, apiURL, browserURL, uuid)
	if err != nil {
		return fmt.Errorf("failed to update projektove organization: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrOrganizationNotFound
	}

	return nil
}

func (r *RepositorySqlite) StoreOrganizationUser(ctx context.Context, orgID int, name string, projektoveID int) error {
	if _, err := r.DB.ExecContext(ctx, "INSERT INTO projektove_organizations_users (uuid, organization_id, name, projektove_id) VALUES (?, ?, ?, ?)", newUUID(), orgID, name, projektoveID); err != nil {
		return fmt.Errorf("failed to store organization user: %w", err)
	}

	return nil
}

func (r *RepositorySqlite) DeleteOrganizationUser(ctx context.Context, orgID int, uuid string) error {
	result, err := r.DB.ExecContext(ctx, "DELETE FROM projektove_organizations_users WHERE uuid = ? AND organization_id = ?", uuid, orgID)
	if err != nil {
		return fmt.Errorf("failed to delete organization user: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrOrganizationNotFound
	}

	return nil
}

func (r *RepositorySqlite) StoreIssue(ctx context.Context, org UserProjektoveOrganization, issue IssueCreate) (int, error) {
	uuid := newUUID()

	query := `INSERT INTO issues (uuid, parent, parent_id, subject, description, project_id, start_date, due_date, assigned_to_id, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, execErr := r.DB.ExecContext(ctx, query, uuid, issue.Parent, issue.ParentID, issue.Subject, issue.Description, issue.ProjectID, issue.StartDate, issue.DueDate, issue.AssignedToID, IssueStatusCreated)
	if execErr != nil {
		return 0, fmt.Errorf("failed to store issue: %w", execErr)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return int(id), nil
}

const issueCols = "id, uuid, status, parent, parent_id, subject, description, project_id, start_date, due_date, assigned_to_id, projektove_id"

func (r *RepositorySqlite) parentBelongsToUser(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int) (bool, error) {
	table := ""
	switch parent {
	case IssueParentBatch:
		table = "issue_batches"
	case IssueParentPrompt:
		table = "prompts"
	default:
		return false, fmt.Errorf("unrecognized parent %q", parent)
	}

	var fetched int
	if err := r.DB.QueryRowContext(ctx, fmt.Sprintf("SELECT id FROM %s WHERE id = ? AND user_projektove_organization_id = ?", table), parentID, org.ID).Scan(&fetched); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("when fetching parent: %w", err)
	}
	return true, nil
}

func (r *RepositorySqlite) UpdateIssue(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int, id int, obj IssueUpdate) error {
	belongs, err := r.parentBelongsToUser(ctx, org, parent, parentID)
	if err != nil {
		return fmt.Errorf("when checking if parent belongs to user: %w", err)
	}

	if !belongs {
		return ErrParentDoesNotBelongToUser
	}

	query := `UPDATE issues SET status = ?, subject = ?, description = ?, project_id = ?, start_date = ?, due_date = ?, assigned_to_id = ?, projektove_id = ? WHERE id = ?`
	result, execErr := r.DB.ExecContext(ctx, query, obj.Status, obj.Subject, obj.Description, obj.ProjectID, obj.StartDate, obj.DueDate, obj.AssignedToID, obj.ProjektoveID, id)
	if execErr != nil {
		return fmt.Errorf("failed to update issue: %w", execErr)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrIssueNotFound
	}

	return nil
}

func (r *RepositorySqlite) ListIssues(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int) ([]Issue, error) {
	issues := []Issue{}

	belongs, err := r.parentBelongsToUser(ctx, org, parent, parentID)
	if err != nil {
		return nil, fmt.Errorf("when checking if parent belongs to user: %w", err)
	}

	if !belongs {
		return nil, ErrParentDoesNotBelongToUser
	}

	rows, err := r.DB.QueryContext(ctx, "SELECT "+issueCols+" FROM issues WHERE parent = ? AND parent_id = ?", parent, parentID)
	if err != nil {
		return nil, fmt.Errorf("when listing issues: %w", err)
	}

	for rows.Next() {
		i := Issue{}
		if err := rows.Scan(&i.ID, &i.UUID, &i.Status, &i.Parent, &i.ParentID, &i.Subject, &i.Description, &i.ProjectID, &i.StartDate, &i.DueDate, &i.AssignedToID, &i.ProjektoveID); err != nil {
			return nil, fmt.Errorf("when scanning results: %w", err)
		}
		issues = append(issues, i)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("when iterating over results: %w", err)
	}

	return issues, nil
}

func (r *RepositorySqlite) GetIssue(ctx context.Context, org UserProjektoveOrganization, parent IssueParent, parentID int, id int) (Issue, error) {
	belongs, err := r.parentBelongsToUser(ctx, org, parent, parentID)
	if err != nil {
		return Issue{}, fmt.Errorf("when checking if parent belongs to user: %w", err)
	}

	if !belongs {
		return Issue{}, ErrParentDoesNotBelongToUser
	}

	i := Issue{}
	err = r.DB.QueryRowContext(ctx, "SELECT "+issueCols+" FROM issues WHERE parent = ? AND parent_id = ? AND id = ?", parent, parentID, id).Scan(&i.ID, &i.UUID, &i.Status, &i.Parent, &i.ParentID, &i.Subject, &i.Description, &i.ProjectID, &i.StartDate, &i.DueDate, &i.AssignedToID, &i.ProjektoveID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Issue{}, ErrIssueNotFound
		}
		return Issue{}, fmt.Errorf("when getting issue: %w", err)
	}

	return i, nil
}

func (r *RepositorySqlite) GetIssueByUUID(ctx context.Context, org UserProjektoveOrganization, uuid string) (Issue, error) {
	row := Issue{}
	err := r.DB.QueryRowContext(ctx, "SELECT "+issueCols+" FROM issues WHERE uuid = ?", uuid).Scan(&row.ID, &row.UUID, &row.Status, &row.Parent, &row.ParentID, &row.Subject, &row.Description, &row.ProjectID, &row.StartDate, &row.DueDate, &row.AssignedToID, &row.ProjektoveID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Issue{}, ErrIssueNotFound
		}
		return Issue{}, fmt.Errorf("when getting issue: %w", err)
	}

	belongs, err := r.parentBelongsToUser(ctx, org, row.Parent, row.ParentID)
	if err != nil {
		return Issue{}, fmt.Errorf("when checking if parent belongs to user: %w", err)
	}
	if !belongs {
		return Issue{}, ErrParentDoesNotBelongToUser
	}

	return row, nil
}
