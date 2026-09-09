CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT,
    is_admin INTEGER NOT NULL CHECK (is_admin IN (0, 1))
);

CREATE TABLE IF NOT EXISTS providers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    provider TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT UNIQUE NOT NULL,
    provider_id INTEGER NOT NULL,
    model TEXT NOT NULL,
    UNIQUE(provider_id, model),
    FOREIGN KEY (provider_id) REFERENCES providers(id)
);

-- describes which projektove organizations the user has added to his profile
CREATE TABLE IF NOT EXISTS users_projektove_organizations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT UNIQUE NOT NULL,
    user_id INTEGER NOT NULL,
    organization_id INTEGER NOT NULL,
    token TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (organization_id) REFERENCES projektove_organizations(id)
);

CREATE TABLE IF NOT EXISTS projektove_organizations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    api_url TEXT NOT NULL,
    browser_url TEXT NOT NULL
);

-- table of projektove users within the organization
CREATE TABLE IF NOT EXISTS projektove_organizations_users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    organization_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    projektove_id INTEGER NOT NULL,
    FOREIGN KEY(organization_id) REFERENCES projektove_organizations(id)
);

CREATE TABLE IF NOT EXISTS users_models (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT UNIQUE NOT NULL,
    user_id INTEGER NOT NULL,
    model_id INTEGER NOT NULL,
    token TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (model_id) REFERENCES models(id)
);

CREATE TABLE IF NOT EXISTS contexts (
    id INTEGER PRIMARY KEY,
    uuid TEXT UNIQUE NOT NULL,
    name TEXT,
    context TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS prompts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT UNIQUE NOT NULL,
    prompt TEXT NOT NULL,
    result TEXT,
    error TEXT,
    context_id INTEGER NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('created', 'processing', 'done', 'error')),
    model_id INTEGER NOT NULL,
    file_content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_projektove_organization_id INTEGER NOT NULL,
    FOREIGN KEY (user_projektove_organization_id) REFERENCES users_projektove_organizations(id),
    FOREIGN KEY (context_id) REFERENCES contexts(id),
    FOREIGN KEY (model_id) REFERENCES models(id)
);

CREATE TABLE IF NOT EXISTS tasks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL,
    payload TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    error TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    finished_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS projects (
    id INTEGER PRIMARY KEY,
    projects TEXT NOT NULL,
    fetched_at TIMESTAMP NOT NULL,
    user_projektove_organization_id INTEGER NOT NULL,
    FOREIGN KEY (user_projektove_organization_id) REFERENCES users_projektove_organizations(id)
);

CREATE TABLE IF NOT EXISTS issue_batches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    uuid TEXT UNIQUE NOT NULL,
    file_content TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    user_projektove_organization_id INTEGER NOT NULL,
    FOREIGN KEY (user_projektove_organization_id) REFERENCES users_projektove_organizations(id)
);

CREATE TABLE IF NOT EXISTS issues (
    id INTEGER PRIMARY KEY,
    uuid TEXT UNIQUE NOT NULL,
    subject TEXT NOT NULL,
    description TEXT NOT NULL,
    project_id INTEGER NOT NULL,
    start_date TIMESTAMP NOT NULL,
    due_date TIMESTAMP,
    assigned_to_id INTEGER,
    status TEXT CHECK (status IN ('created', 'submitted', 'submit_failed', 'ignored')),
    projektove_id INTEGER,
    parent TEXT NOT NULL CHECK (parent IN ('prompt', 'batch')),
    parent_id INTEGER NOT NULL
);
