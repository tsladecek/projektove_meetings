# Meetings -> Projektove

A single-page web app that turns meeting notes into [Projektove](https://projektove.cz) issues.

You upload meeting minutes (as an unstructured document or a structured CSV table) and the app
extracts the issues from them, lets you review and edit everything, and then submits them to
your Projektove instance — one by one or all at once.

## tldr.

```
mkdir data
docker run -v ./data/:/data --env DB=/data/db.sqlite --env AUTH_DEFAULTUSER=admin@admin.com --env AUTH_DEFAULTPASSWORD=password --env AUTH_SECRETKEY=secret -p 8000:8000 ghcr.io/tsladecek/projektove_meetings
```

## What it does

1. **Authenticate** via an OIDC provider (e.g. Keycloak) or Self auth `admin@admin.com:password`. On first login, set up the app on
   the **User** page: store your Projektove token, add LLM models (provider + model + token),
   and define reusable *contexts* (custom instructions that shape how the LLM extracts issues).

2. **Upload meeting notes** through one of two flows:

   - **Prompts** — for unstructured meeting notes. You attach the notes, pick a
     context, and the LLM extracts the issues from them.
   - **Batches** — for structured tables. You upload a CSV with the required columns
     (the server validates the file before importing; the raw file content is stored as well).

3. **Review the result**, on the prompt or batch detail page. Each extracted issue shows subject,
   description, project, assignee, and start/due dates. Issues stay editable until submitted, and
   can be ignored.

4. **Submit to Projektove.** Send issues one by one or via "Submit all". Once an issue is
   successfully created, its status turns `submitted`, the Projektove link is stored, and the
   issue becomes read-only. Failures are clearly reported per issue.

## How it works

- Frontend built with **HTMX v4**; all UI is rendered server-side with **gomponents**
  and styled with **Tailwind** (utility classes only, no custom CSS rules).
- A background queue worker (an SQLite-backed `tasks` table) asynchronously runs the LLM
  inference, so prompts page shows a spinner until the result is ready. Submissions are
  independent of the UI and retried on failure.
- **SQLite** stores the data; the app is just a Go binary plus the `static/` assets.
- Everything is generated from the database through the controller usecases in `usecases.go`,
  with HTTP endpoints backed by `Controller` (see `web.go`).

## Development

Requirements: Go, Tailwind CSS (`tailwindcss` on `PATH`), Make, and optionally `sqlite3` for
inspecting the database file.

```shell
go mod download
make tw       # watches static/css/input.css and regenerates output.css
make dev      # runs air (hot reload) — starts the server on :8000
```

> The Tailwind `output.css` is a committed build artifact. `make tw` rebuilds it continuously;
> if you introduce a new utility class, regenerate it once with
> `tailwindcss -i ./static/css/input.css -o ./static/css/output.css`.

### Configuration

Configuration comes from a TOML file (pass it with `-c config.toml`) and may be overridden by
environment variables (`config_dev.toml` is the default dev setup). The app requires:

- **`[oidc]`** — issuer, client id/secret, and cookie names for the OIDC login flow.
- **`[projektove]`** — the Projektove instance URL, the URL template used to link created
  issues (e.g. `https://app.projektove.cz/.../tasks/%d`), and the list of users (id + name)
  that can be assigned as issue assignees.

See [config.go](config.go) for all fields, defaults, and their environment variable names.

### Resetting the database

Development data lives in the default SQLite file `db.sqlite` (override with `DB`).
To reset it, stop the server and delete `db.sqlite*`; migrations run automatically on startup.
