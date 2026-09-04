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
