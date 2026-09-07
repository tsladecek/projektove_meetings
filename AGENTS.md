# Overview

The purpose of this project is to extract issues from meeting notes and submit them to the Projektove issues tracker

# API

This should be a SPA written with HTMX v4. The main navigation is on the sidebar with following links:
- user page
- new prompt
- prompts

## Pages

### /user
- update current user
- add new contexts
- add new llm models

### /prompts
- lists all meetings with links to meeting page

### /prompts/new
- user uploads meeting notes
- user picks context from a list fetched from db
- The prompt is stored with status `created` and an inference task is enqueued in the SQLite-backed `tasks` table
- The user is redirected immediately to the prompts/{id} page

### /prompts/{id}
- While the prompt status is `created`/`processing`, the page polls itself with HTMX `hx-trigger="every 5s"` (only `#prompt-view` fragment via the `HX-Request` header), showing a spinner until inference finishes
- A background queue worker (`queue.go`, `InferenceQueue`) claims tasks, runs the LLM inference and writes issues + the final status (`done`/`error`)
- In this page the user can see the prompt that was submitted, its result and the issues that were parsed
- The user must have the option to update the issues unless they have been submitted successfully
- The user must have the option to submit the issues one by one or all at once. Nevertheless, the UI should nicely render this with per issue loading and at the end either display per issue error or a green check icon
- After issue submission, its status should be updated. If successfull the projektove_id should be filled as well

# Structure

- use gomponents for creating new components inside the `components` struct
- endpoints must call controller usecases at `./usecases.go`
- style with tailwind. Keep it as minimal as possible
- all views that are generated from db should be declared in `./views.go`

