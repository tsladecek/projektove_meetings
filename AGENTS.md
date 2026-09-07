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
- the Infer usecases is run which calls the llm to parse the meeting notes to issues
- After successful parsing the user is redirected to the prompts/{id} page

### /prompts/{id}
- In this page the user can see the prompt that was submitted, its result and the issues that were parsed
- The user must have the option to update the issues unless they have been submitted successfully
- The user must have the option to submit the issues one by one or all at once. Nevertheless, the UI should nicely render this with per issue loading and at the end either display per issue error or a green check icon
- After issue submission, its status should be updated. If successfull the projektove_id should be filled as well

# Structure

- use gomponents for creating new components inside the `components` struct
- endpoints must call controller usecases at `./usecases.go`
- style with tailwind. Keep it as minimal as possible
- all views that are generated from db should be declared in `./views.go`

# TODO
Known caveats / things to verify next time
1. Submit uses DB values, not form values. The editable issue card is wrapped in a <form> (so hx-include="closest form" sends prompt_id + fields), but Controller.SubmitIssue reads the issue from DB, ignores form subject/project/etc. If a user edits but doesn't Save before Submit, the DB values win. (Acceptable; noted during design.)
2. X-Error/X-Toast headers + always-200 pattern deliberately avoid HTMX swapping plain error text into the card target. This is the intended UX.
3. Submit-all: per-issue Submit buttons carry [data-submit-issue]; the "Submit all pending issues" button does NOT (avoiding recursion in submitAll()). Its row (#submit-all-row) is hidden once no pending buttons remain.
4. Notification… none. listContexts GET /api/contexts handler (api.listContexts()) is still a 501 stub — was never wired to a page (contexts live on /user).
5. a.notImplemented helper remains and is only used by listContexts.
Still to do (roadmap in AGENTS.md, out of session scope)
- /prompts page: api.prompts() still renders "Prompts list not implemented yet." It should list all meetings via a ListPrompts usecase + links to /prompts/{id} (the prompts: end(GET, "/prompts/") endpoint exists).
Restart commands
cd /home/tomas/Desktop/projects/projektove_meeting
go build ./... && go vet ./... && go test ./...
Run the app with go run ./cmd (check cmd/main.go for config/flags — user project; standard Projektove/LLM config from env/file).
