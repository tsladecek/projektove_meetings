package projektovemeeting

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	"maragu.dev/gomponents-heroicons/v3/solid"
	htmx "maragu.dev/gomponents-htmx"
	co "maragu.dev/gomponents/components"
	h "maragu.dev/gomponents/html"
)

//go:embed static/*
var staticFS embed.FS

const baseButtonClass = "inline-flex items-center justify-center gap-1 rounded font-medium cursor-pointer disabled:cursor-not-allowed disabled:opacity-60 "

type api struct {
	controller Controller
	basePath   string
	components components
}

func NewHandler(auth Auth, baseURL, cookieName string, controller Controller, projektoveIssueEndpoint, logoutEndpoint string) http.Handler {
	m := http.NewServeMux()

	burl, err := url.Parse(baseURL)
	if err != nil {
		panic(err)
	}

	end := func(method, endpoint string) Endpoint {
		return NewEndpoint(method, burl.Path, endpoint)
	}

	endAPI := func(method, endpoint string) Endpoint {
		return NewEndpoint(method, path.Join(burl.Path, "/api"), endpoint)
	}

	e := endpoints{
		root:   end(http.MethodGet, "/"),
		static: end(http.MethodGet, "/static/"),
		logout: end(http.MethodGet, logoutEndpoint),

		// pages
		user:      end(http.MethodGet, "/user"),
		prompts:   end(http.MethodGet, "/prompts/"),
		newPrompt: end(http.MethodGet, "/prompts/new"),
		prompt:    end(http.MethodGet, "/prompts/{id}"),
		batches:   end(http.MethodGet, "/batches/"),
		newBatch:  end(http.MethodGet, "/batches/new"),
		batch:     end(http.MethodGet, "/batches/{id}"),

		// api

		updateUser:    endAPI(http.MethodPut, "/user"),
		addContext:    endAPI(http.MethodPost, "/contexts"),
		addLLMModel:   endAPI(http.MethodPost, "/models"),
		deleteContext: endAPI(http.MethodDelete, "/contexts/{id}"),

		listContexts: endAPI(http.MethodGet, "/contexts"),
		createPrompt: endAPI(http.MethodPost, "/prompts"),
		createBatch:  endAPI(http.MethodPost, "/batches"),

		updateIssue: endAPI(http.MethodPut, "/issues/{id}"),
		submitIssue: endAPI(http.MethodPost, "/issues/{id}/submit"),
		deleteIssue: endAPI(http.MethodDelete, "/issues/{id}"),
	}

	c := components{endpoints: e, projektoveIssueEndpoint: projektoveIssueEndpoint}
	a := api{controller: controller, basePath: burl.Path, components: c}

	type endpointHandler struct {
		endpoint Endpoint
		handler  http.Handler
	}

	for _, eh := range []endpointHandler{
		{endpoint: e.root, handler: a.root()},
		{endpoint: e.static, handler: a.static()},

		// pages
		{endpoint: e.user, handler: a.user()},
		{endpoint: e.prompts, handler: a.prompts()},
		{endpoint: e.newPrompt, handler: a.newPrompt()},
		{endpoint: e.prompt, handler: a.prompt()},
		{endpoint: e.batches, handler: a.batches()},
		{endpoint: e.newBatch, handler: a.newBatch()},
		{endpoint: e.batch, handler: a.batch()},

		// api
		{endpoint: e.updateUser, handler: a.updateUser()},
		{endpoint: e.addContext, handler: a.addContext()},
		{endpoint: e.deleteContext, handler: a.deleteContext()},
		{endpoint: e.addLLMModel, handler: a.addLLMModel()},
		{endpoint: e.listContexts, handler: a.listContexts()},
		{endpoint: e.createPrompt, handler: a.createPrompt()},
		{endpoint: e.createBatch, handler: a.createBatch()},
		{endpoint: e.updateIssue, handler: a.updateIssue()},
		{endpoint: e.submitIssue, handler: a.submitIssue()},
		{endpoint: e.deleteIssue, handler: a.deleteIssue()},
	} {
		m.Handle(eh.endpoint.Pattern(), auth.Middleware(eh.handler))
	}

	auth.RegisterRoutes(m)

	handler := MiddlewareLogging(m)
	return handler
}

func (a api) root() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.components.Page(a.components.RootPage()).Render(w)
	}
}

func (a api) static() http.HandlerFunc {
	fs := http.FileServer(http.FS(staticFS))
	handler := http.StripPrefix(a.basePath, fs)

	return func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r)
	}

}

func withSuccessToast(w http.ResponseWriter) {
	w.Header().Add("X-Toast", "Success!")
}

// pages

func (a api) user() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		profile, err := a.controller.GetUserProfile(r.Context(), user)
		if err != nil {
			WriteError(w, "failed to load user", http.StatusInternalServerError, err)
			return
		}

		a.components.Page(a.components.UserPage(profile)).Render(w)
	}
}

const promptsPageSize = 20

func (a api) prompts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				WriteError(w, "invalid offset", http.StatusBadRequest, nil)
				return
			}
			offset = parsed
		}

		view, err := a.controller.ListPrompts(r.Context(), user, promptsPageSize, offset)
		if err != nil {
			WriteError(w, "failed to load prompts", http.StatusInternalServerError, err)
			return
		}

		if r.Header.Get("HX-Request") != "" {
			a.components.PromptsBatch(view).Render(w)
			return
		}
		a.components.Page(a.components.PromptsPage(view)).Render(w)
	}
}

func (a api) newPrompt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		contexts, err := a.controller.ListContexts(r.Context(), user)
		if err != nil {
			WriteError(w, "failed to load contexts", http.StatusInternalServerError, err)
			return
		}

		a.components.Page(a.components.NewPromptPage(contexts, user.LLMModels, nil)).Render(w)
	}
}

func (a api) prompt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		id := r.PathValue("id")

		view, err := a.controller.GetPrompt(r.Context(), user, id)
		if err != nil {
			if errors.Is(err, ErrPromptNotFound) {
				WriteError(w, "prompt not found", http.StatusNotFound, nil)
				return
			}
			WriteError(w, "failed to load prompt", http.StatusInternalServerError, err)
			return
		}

		projects, err := a.controller.ListProjects(r.Context(), user)
		if err != nil {
			projects = []ProjectOptionView{}
		}

		fragment := a.components.PromptFragment(view, projects, a.controller.Users)
		if r.Header.Get("HX-Request") != "" {
			fragment.Render(w)
			return
		}
		a.components.Page(fragment).Render(w)
	}
}

func (a api) batches() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				WriteError(w, "invalid offset", http.StatusBadRequest, nil)
				return
			}
			offset = parsed
		}

		view, err := a.controller.ListBatches(r.Context(), user, promptsPageSize, offset)
		if err != nil {
			WriteError(w, "failed to load batches", http.StatusInternalServerError, err)
			return
		}

		if r.Header.Get("HX-Request") != "" {
			a.components.BatchesList(view).Render(w)
			return
		}
		a.components.Page(a.components.BatchesPage(view)).Render(w)
	}
}

func (a api) newBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.components.Page(a.components.NewBatchPage(nil)).Render(w)
	}
}

func (a api) batch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		id := r.PathValue("id")

		view, err := a.controller.GetBatch(r.Context(), user, id)
		if err != nil {
			if errors.Is(err, ErrBatchNotFound) {
				WriteError(w, "batch not found", http.StatusNotFound, nil)
				return
			}
			WriteError(w, "failed to load batch", http.StatusInternalServerError, err)
			return
		}

		projects, err := a.controller.ListProjects(r.Context(), user)
		if err != nil {
			projects = []ProjectOptionView{}
		}

		fragment := a.components.BatchFragment(view, projects, a.controller.Users)
		if r.Header.Get("HX-Request") != "" {
			fragment.Render(w)
			return
		}
		a.components.Page(fragment).Render(w)
	}
}

// api stubs

func (a api) updateUser() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		if err := r.ParseForm(); err != nil {
			WriteError(w, "invalid form", http.StatusBadRequest, err)
			return
		}

		v := UserUpdateView{
			ProjektoveToken: r.Form.Get("projektove_token"),
			Models:          []LLMModelView{},
		}

		providers := r.Form["model_provider"]
		names := r.Form["model_name"]
		tokens := r.Form["model_token"]
		for i := range providers {
			name := ""
			token := ""
			if i < len(names) {
				name = names[i]
			}
			if i < len(tokens) {
				token = tokens[i]
			}
			v.Models = append(v.Models, LLMModelView{
				Provider: LLMProvider(providers[i]),
				Model:    name,
				Token:    token,
			})
		}

		if err := a.controller.UpdateUser(r.Context(), user, v); err != nil {
			WriteError(w, "failed to update user", http.StatusInternalServerError, err)
			return
		}

		withSuccessToast(w)
	}
}

func (a api) addContext() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		if err := r.ParseForm(); err != nil {
			WriteError(w, "invalid form", http.StatusBadRequest, err)
			return
		}

		c := LLMContextCreate{Name: r.Form.Get("name"), Context: r.Form.Get("context")}
		cv, err := a.controller.StoreContext(r.Context(), user, c)
		if err != nil {
			WriteError(w, "failed to add context", http.StatusInternalServerError, err)
			return
		}

		a.components.ContextRow(cv).Render(w)
	}
}

func (a api) deleteContext() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		id := r.PathValue("id")

		if err := a.controller.DeleteContext(r.Context(), user, id); err != nil {
			switch {
			case errors.Is(err, ErrContextInUse):
				WriteError(w, "context is in use by a prompt", http.StatusConflict, nil)
			case errors.Is(err, ErrContextNotFound):
				WriteError(w, "context not found", http.StatusNotFound, nil)
			default:
				WriteError(w, "failed to delete context", http.StatusInternalServerError, err)
			}
			return
		}

		withSuccessToast(w)
	}
}

func (a api) addLLMModel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			WriteError(w, "invalid form", http.StatusBadRequest, err)
			return
		}

		m := LLMModelView{
			Provider: LLMProvider(r.Form.Get("provider")),
			Model:    r.Form.Get("model"),
			Token:    r.Form.Get("token"),
		}
		a.components.ModelRow(m).Render(w)
	}
}

func (a api) listContexts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.notImplemented(w, "list contexts not implemented")
	}
}

func (a api) createPrompt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			WriteError(w, "invalid request", http.StatusBadRequest, err)
			return
		}

		contextUUID := r.Form.Get("context_id")

		context, err := a.controller.ResolveContext(r.Context(), user, contextUUID)
		if err != nil {
			WriteError(w, "invalid context", http.StatusBadRequest, err)
			return
		}

		model := r.Form.Get("model")
		parts := strings.SplitN(model, "|", 2)
		if len(parts) != 2 {
			WriteError(w, "invalid model", http.StatusBadRequest, nil)
			return
		}
		provider, name := parts[0], parts[1]

		file, _, err := r.FormFile("meeting")
		if err != nil {
			WriteError(w, "invalid meeting file", http.StatusBadRequest, err)
			return
		}
		defer file.Close()

		meeting, err := io.ReadAll(file)
		if err != nil {
			WriteError(w, "failed to read meeting file", http.StatusBadRequest, err)
			return
		}

		promptUUID, err := a.controller.CreatePrompt(r.Context(), user, provider, name, context.ID, string(meeting))
		if err != nil {
			if errors.Is(err, ErrProjektoveTokenNotConfigured) {
				contexts, ctxErr := a.controller.ListContexts(r.Context(), user)
				if ctxErr != nil {
					WriteError(w, "failed to load contexts", http.StatusInternalServerError, ctxErr)
					return
				}
				a.components.Page(a.components.NewPromptPage(contexts, user.LLMModels, []string{
					"Your Projektove token is not configured. Set it on the User page before creating a prompt.",
				})).Render(w)
				return
			}
			if errors.Is(err, ErrModelNotFound) {
				WriteError(w, "llm model not found", http.StatusNotFound, err)
				return
			}
			WriteError(w, "failed to create prompt", http.StatusInternalServerError, err)
			return
		}

		redirectPath := strings.Replace(a.components.endpoints.prompt.Path(), "{id}", promptUUID, 1)
		http.Redirect(w, r, redirectPath, http.StatusSeeOther)
	}
}

func (a api) createBatch() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		if err := r.ParseMultipartForm(10 << 20); err != nil {
			WriteError(w, "invalid request", http.StatusBadRequest, err)
			return
		}

		file, _, err := r.FormFile("csv")
		if err != nil {
			WriteError(w, "invalid csv file", http.StatusBadRequest, err)
			return
		}
		defer file.Close()

		raw, err := io.ReadAll(file)
		if err != nil {
			WriteError(w, "failed to read csv file", http.StatusBadRequest, err)
			return
		}

		batchUUID, err := a.controller.CreateBatch(r.Context(), user, string(raw))
		if err != nil {
			if errors.Is(err, ErrProjektoveTokenNotConfigured) {
				a.components.Page(a.components.NewBatchPage([]string{
					"Your Projektove token is not configured. Set it on the User page before uploading a batch.",
				})).Render(w)
				return
			}

			var csvErr *BatchCSVError
			if errors.As(err, &csvErr) {
				a.components.Page(a.components.NewBatchPage(csvErr.Messages)).Render(w)
				return
			}
			WriteError(w, "failed to create batch", http.StatusInternalServerError, err)
			return
		}

		redirectPath := strings.Replace(a.components.endpoints.batch.Path(), "{id}", batchUUID, 1)
		http.Redirect(w, r, redirectPath, http.StatusSeeOther)
	}
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse("2006-01-02", s)
}

func parseIssueFields(r *http.Request) IssueUpdateView {
	projectID, _ := strconv.Atoi(r.Form.Get("project_id"))
	assigneeID, _ := strconv.Atoi(r.Form.Get("assigned_to_id"))
	startDate, _ := parseDate(r.Form.Get("start_date"))
	dueDate, _ := parseDate(r.Form.Get("due_date"))

	return IssueUpdateView{
		Subject:      r.Form.Get("subject"),
		Description:  r.Form.Get("description"),
		ProjectID:    projectID,
		AssignedToID: assigneeID,
		StartDate:    startDate,
		DueDate:      dueDate,
	}
}

func (a api) renderIssueCard(ctx context.Context, w http.ResponseWriter, user User, issueUUID string, projects []ProjectOptionView, errMsg string) {
	iss, err := a.controller.GetIssueViewByUUID(ctx, user, issueUUID)
	if err != nil {
		WriteError(w, "issue not found", http.StatusNotFound, nil)
		return
	}

	iss.Error = errMsg
	a.components.IssueCard(iss, projects, a.controller.Users).Render(w)
}

func (a api) updateIssue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		if err := r.ParseForm(); err != nil {
			WriteError(w, "invalid form", http.StatusBadRequest, err)
			return
		}

		issueUUID := r.PathValue("id")

		v := parseIssueFields(r)

		if err := a.controller.UpdateIssue(r.Context(), user, issueUUID, v); err != nil {
			switch {
			case errors.Is(err, ErrIssueSubmitted):
				projects, _ := a.controller.ListProjects(r.Context(), user)
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
				return
			case errors.Is(err, ErrIssueNotFound), errors.Is(err, ErrParentDoesNotBelongToUser):
				WriteError(w, "issue not found", http.StatusNotFound, nil)
				return
			default:
				projects, _ := a.controller.ListProjects(r.Context(), user)
				w.Header().Set("X-Error", "Failed to update issue")
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "Failed to update issue")
				return
			}
		}

		withSuccessToast(w)
		projects, _ := a.controller.ListProjects(r.Context(), user)
		a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
	}
}

func (a api) submitIssue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		if err := r.ParseForm(); err != nil {
			WriteError(w, "invalid form", http.StatusBadRequest, err)
			return
		}

		issueUUID := r.PathValue("id")

		v := parseIssueFields(r)
		projects, _ := a.controller.ListProjects(r.Context(), user)

		if err := a.controller.UpdateIssue(r.Context(), user, issueUUID, v); err != nil {
			switch {
			case errors.Is(err, ErrIssueSubmitted):
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
				return
			case errors.Is(err, ErrIssueNotFound), errors.Is(err, ErrParentDoesNotBelongToUser):
				WriteError(w, "issue not found", http.StatusNotFound, nil)
				return
			default:
				w.Header().Set("X-Error", "Failed to update issue")
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "Failed to update issue")
				return
			}
		}

		if err := a.controller.SubmitIssue(r.Context(), user, issueUUID); err != nil {
			switch {
			case errors.Is(err, ErrIssueSubmitted):
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
				return
			case errors.Is(err, ErrIssueNotFound), errors.Is(err, ErrParentDoesNotBelongToUser):
				WriteError(w, "issue not found", http.StatusNotFound, nil)
				return
			case errors.Is(err, ErrIssueIncomplete):
				w.Header().Set("X-Error", "Cannot submit: missing required fields")
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "Cannot submit: missing required fields")
				return
			default:
				w.Header().Set("X-Error", "Failed to submit issue")
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "Failed to submit issue")
				return
			}
		}

		withSuccessToast(w)
		a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
	}
}

func (a api) deleteIssue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, "user not found", http.StatusUnauthorized, nil)
			return
		}

		issueUUID := r.PathValue("id")
		projects, _ := a.controller.ListProjects(r.Context(), user)

		if err := a.controller.DeleteIssue(r.Context(), user, issueUUID); err != nil {
			switch {
			case errors.Is(err, ErrIssueSubmitted):
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
				return
			case errors.Is(err, ErrIssueDeleted):
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
				return
			case errors.Is(err, ErrIssueNotFound), errors.Is(err, ErrParentDoesNotBelongToUser):
				WriteError(w, "issue not found", http.StatusNotFound, nil)
				return
			default:
				w.Header().Set("X-Error", "Failed to delete issue")
				a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
				return
			}
		}

		withSuccessToast(w)
		a.renderIssueCard(r.Context(), w, user, issueUUID, projects, "")
	}
}

type endpoints struct {
	root   Endpoint
	static Endpoint
	logout Endpoint

	// pages
	user      Endpoint
	prompts   Endpoint
	newPrompt Endpoint
	prompt    Endpoint
	batches   Endpoint
	newBatch  Endpoint
	batch     Endpoint

	// api - should have /api prefix
	updateUser    Endpoint
	addContext    Endpoint
	deleteContext Endpoint
	addLLMModel   Endpoint

	listContexts Endpoint
	createPrompt Endpoint
	createBatch  Endpoint

	updateIssue Endpoint
	submitIssue Endpoint
	deleteIssue Endpoint
}

type components struct {
	endpoints               endpoints
	projektoveIssueEndpoint string
}

func (c components) Page(body g.Node) g.Node {
	return co.HTML5(
		co.HTML5Props{
			Title:       "Meetings",
			Description: "Web service for transcription of meetings to Projektove Issues",
			Language:    "en",
			Head: []g.Node{
				h.Link(h.Rel("stylesheet"), h.Href(path.Join(c.endpoints.static.Path(), "css", "output.css"))),
				h.Link(h.Rel("stylesheet"), h.Href(path.Join(c.endpoints.static.Path(), "css", "toastify.min.css"))),
				h.Link(h.Rel("stylesheet"), h.Href(path.Join(c.endpoints.static.Path(), "css", "tippy.css"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "htmx.min.js"))),
			},
			Body: []g.Node{
				h.Div(
					h.Class("w-screen h-screen flex"),
					c.Sidebar(),
					h.Main(
						h.Class("flex-1 overflow-auto p-6"),
						body,
					),
				),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "popper.min.js"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "tippy-bundle.umd.min.js"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "toastify.js"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "general.js"))),
			},
		},
	)
}

type navLink struct {
	label string
	href  string
	icon  g.Node
}

func (c components) Sidebar() g.Node {
	groups := [][]navLink{
		{
			{label: "User", href: c.endpoints.user.Path(), icon: solid.User(h.Class("h-5 w-5 shrink-0"))},
		},
		{
			{label: "New prompt", href: c.endpoints.newPrompt.Path(), icon: solid.PlusCircle(h.Class("h-5 w-5 shrink-0"))},
			{label: "Prompts", href: c.endpoints.prompts.Path(), icon: solid.DocumentText(h.Class("h-5 w-5 shrink-0"))},
		},
		{
			{label: "New batch", href: c.endpoints.newBatch.Path(), icon: solid.ArrowUpTray(h.Class("h-5 w-5 shrink-0"))},
			{label: "Batches", href: c.endpoints.batches.Path(), icon: solid.CircleStack(h.Class("h-5 w-5 shrink-0"))},
		},
	}

	navBlocks := []g.Node{}
	for i, grp := range groups {
		if i > 0 {
			navBlocks = append(navBlocks, h.Hr(h.Class("border-gray-700 my-2")))
		}

		items := []g.Node{}
		for _, l := range grp {
			items = append(items, h.A(
				h.Href(l.href),
				h.Title(l.label),
				h.Class("flex items-center justify-center md:justify-start gap-3 w-full px-2 py-2 rounded hover:bg-gray-700"),
				l.icon,
				h.Span(h.Class("hidden md:inline"), g.Text(l.label)),
			))
		}
		navBlocks = append(navBlocks, h.Nav(h.Class("space-y-1 w-full"), g.Group(items)))
	}

	navBlocks = append(navBlocks,
		h.Div(h.Class("flex-1")),
		h.Hr(h.Class("border-gray-700 my-2")),
		h.Nav(h.Class("space-y-1 w-full"),
			h.A(
				h.Href(c.endpoints.logout.Path()),
				h.Title("Logout"),
				h.Class("flex items-center justify-center md:justify-start gap-3 w-full px-2 py-2 rounded hover:bg-gray-700"),
				solid.ArrowRightOnRectangle(h.Class("h-5 w-5 shrink-0")),
				h.Span(h.Class("hidden md:inline"), g.Text("Logout")),
			),
		),
	)

	return h.Aside(
		h.Class("w-16 md:w-56 bg-gray-800 text-white flex flex-col items-center md:items-stretch p-4"),
		g.Group(navBlocks),
	)
}

func (c components) RootPage() g.Node {
	step := func(num, title, text string) g.Node {
		return h.Li(
			h.Class("flex items-start gap-3"),
			h.Span(
				h.Class("flex items-center justify-center h-6 w-6 rounded-full bg-gray-800 text-white text-sm font-medium shrink-0"),
				g.Text(num),
			),
			h.Div(
				h.H3(h.Class("font-semibold"), g.Text(title)),
				h.P(h.Class("text-sm text-gray-500"), g.Text(text)),
			),
		)
	}

	card := func(href, title, text string, icon g.Node) g.Node {
		return h.A(
			h.Href(href),
			h.Div(
				h.Class("border rounded p-4 hover:bg-gray-100 flex flex-col gap-2"),
				h.Div(
					h.Class("flex items-center gap-2"),
					icon,
					h.H2(h.Class("text-lg font-semibold"), g.Text(title)),
				),
				h.P(h.Class("text-sm text-gray-500"), g.Text(text)),
				h.Span(
					h.Class("text-sm font-medium flex items-center gap-1 mt-1"),
					g.Text("Open"),
					solid.ArrowRight(h.Class("h-4 w-4")),
				),
			),
		)
	}

	return h.Div(
		h.Class("max-w-3xl space-y-8"),

		h.Div(
			h.H1(h.Class("text-2xl font-bold"), g.Text("Meetings to Issues")),
			h.P(
				h.Class("text-gray-500 mt-1"),
				g.Text("Turn meeting notes and CSV tables into Projektove issues. Review, edit and submit them in one place."),
			),
		),

		h.Div(
			h.H2(h.Class("text-lg font-semibold mb-3"), g.Text("How it works")),
			h.Ol(
				h.Class("space-y-3"),
				step("1", "Upload", "Import meeting notes (.txt/.md) or a CSV table of issues."),
				step("2", "Review and edit", "Parsed issues are listed and can be edited before anything is submitted."),
				step("3", "Submit", "Send issues to the Projektove tracker, one by one or all at once."),
				step("4", "Track", "See the submission status on every prompt and batch."),
			),
		),

		h.Div(
			h.H2(h.Class("text-lg font-semibold mb-3"), g.Text("Get started")),
			h.Div(
				h.Class("grid lg:grid-cols-2 gap-4"),
				card(
					c.endpoints.newPrompt.Path(),
					"From meeting notes",
					"Upload meeting notes and an LLM extracts action items into issues, which you then review and submit.",
					solid.DocumentText(h.Class("h-6 w-6 shrink-0")),
				),
				card(
					c.endpoints.newBatch.Path(),
					"From a CSV table",
					"Upload a CSV table of issues. It is validated before the issues are created, then you review and submit them.",
					solid.ArrowUpTray(h.Class("h-6 w-6 shrink-0")),
				),
			),
		),

		h.Div(
			h.Class("text-sm text-gray-500"),
			g.Text("Before you start, set your "),
			h.A(h.Href(c.endpoints.user.Path()), h.Class("underline"), g.Text("Projektove token and LLM model")),
			g.Text(" on the User page."),
		),
	)
}

func (c components) PromptsPage(view PromptListView) g.Node {
	return h.Div(
		h.H1(h.Class("text-2xl font-bold mb-4"), g.Text("Prompts")),
		c.PromptsBatch(view),
	)
}

func (c components) PromptsBatch(view PromptListView) g.Node {
	if len(view.Items) == 0 {
		return h.P(h.Class("text-gray-500"), g.Text("No prompts yet."))
	}

	rows := []g.Node{}
	for _, it := range view.Items {
		rows = append(rows, c.promptRow(it))
	}

	if view.HasMore {
		rows = append(rows, c.loadMoreButton(c.endpoints.prompts.Path()+"?offset="+strconv.Itoa(view.NextOffset)))
	}

	return h.Div(g.Group(rows), h.Class("flex flex-col gap-2"))
}

func (c components) promptRow(it PromptListItem) g.Node {
	path := strings.Replace(c.endpoints.prompt.Path(), "{id}", it.ID, 1)

	statusText, statusClass := promptStatusDisplay(it.Status)

	issues := g.Node(h.Span(h.Class("text-sm text-gray-500"), g.Text("")))
	if it.TotalIssues > 0 {
		if it.SubmittedIssues == it.TotalIssues {
			issues = h.Span(
				h.Class("flex items-center gap-1 text-sm text-green-700"),
				solid.CheckCircle(h.Class("h-5 w-5 text-green-600")),
				g.Text("All submitted"),
			)
		} else {
			issues = h.Span(h.Class("text-sm text-gray-500 text-center"), g.Text(fmt.Sprintf("%d / %d submitted", it.SubmittedIssues, it.TotalIssues)))
		}
	}

	return h.A(
		h.Href(path),
		h.Div(
			h.Class("border hover:bg-gray-100 grid grid-rows-4 justify-center lg:grid-rows-1 lg:grid-cols-4 rounded px-3 py-2 items-center"),
			h.Div(
				h.Class("block min-w-0"),
				h.Div(h.Class("font-sm"), g.Text(it.ID)),
				h.Div(h.Class("text-sm text-gray-500 truncate text-center lg:text-left"), g.Text(it.ContextName)),
			),
			h.Div(h.Class("text-sm text-gray-500 whitespace-nowrap text-center"), g.Text(it.CreatedAt.Format("2006-01-02 15:04"))),
			h.Span(
				h.Class("whitespace-nowrap text-center "+statusClass),
				g.Text(statusText),
			),
			issues,
		))
}

func (c components) BatchesPage(view BatchListView) g.Node {
	return h.Div(
		h.H1(h.Class("text-2xl font-bold mb-4"), g.Text("Batches")),
		c.BatchesList(view),
	)
}

func (c components) BatchesList(view BatchListView) g.Node {
	if len(view.Items) == 0 {
		return h.P(h.Class("text-gray-500"), g.Text("No batches yet."))
	}

	rows := []g.Node{}
	for _, it := range view.Items {
		rows = append(rows, c.batchRow(it))
	}

	if view.HasMore {
		rows = append(rows, c.loadMoreButton(c.endpoints.batches.Path()+"?offset="+strconv.Itoa(view.NextOffset)))
	}

	return h.Div(g.Group(rows), h.Class("flex flex-col gap-2"))
}

func (c components) batchRow(it BatchListItem) g.Node {
	path := strings.Replace(c.endpoints.batch.Path(), "{id}", it.ID, 1)

	issues := g.Node(h.Span(h.Class("text-sm text-gray-500"), g.Text("")))
	if it.TotalIssues > 0 {
		if it.SubmittedIssues == it.TotalIssues {
			issues = h.Span(
				h.Class("flex items-center gap-1 text-sm text-green-700"),
				solid.CheckCircle(h.Class("h-5 w-5 text-green-600")),
				g.Text("All submitted"),
			)
		} else {
			issues = h.Span(h.Class("text-sm text-gray-500 text-center"), g.Text(fmt.Sprintf("%d / %d submitted", it.SubmittedIssues, it.TotalIssues)))
		}
	}

	return h.A(
		h.Href(path),
		h.Div(
			h.Class("border hover:bg-gray-100 grid grid-rows-3 justify-center lg:grid-rows-1 lg:grid-cols-3 rounded px-3 py-2 items-center"),
			h.Div(
				h.Class("block min-w-0"),
				h.Div(h.Class("font-small"), g.Text(it.ID)),
			),
			h.Div(h.Class("text-sm text-gray-500 whitespace-nowrap text-center"), g.Text(it.CreatedAt.Format("2006-01-02 15:04"))),
			issues,
		))
}

func (c components) loadMoreButton(path string) g.Node {
	return c.button(
		baseButtonClass+"bg-gray-800 hover:bg-gray-900 text-white px-4 py-2 disabled:bg-gray-400 disabled:hover:bg-gray-400",
		htmx.Get(path),
		htmx.Target("this"),
		htmx.Swap("outerHTML"),
		htmx.Indicator("#load-more-indicator"),
		g.Text("Load more"),
		h.Span(
			h.ID("load-more-indicator"),
			h.Class("htmx-indicator inline-flex items-center gap-1"),
			solid.ArrowPath(h.Class("h-4 w-4 animate-spin")),
		),
	)
}

func promptStatusDisplay(status PromptStatus) (string, string) {
	switch status {
	case PromptStatusCreated, PromptStatusProcessing:
		return "Processing", "text-amber-600"
	case PromptStatusDone:
		return "Done", "text-green-700"
	case PromptStatusError:
		return "Error", "text-red-700"
	default:
		return string(status), "text-gray-500"
	}
}

func (c components) UserPage(profile UserProfileView) g.Node {
	modelRows := []g.Node{}
	for _, m := range profile.Models {
		modelRows = append(modelRows, c.ModelRow(LLMModelView{Provider: m.Provider, Model: m.Model, Token: m.Token}))
	}

	return h.Div(
		h.H1(h.Class("text-2xl font-bold mb-6"), g.Text("User")),

		h.Form(
			h.ID("user-form"),
			h.Method("post"),
			htmx.Put(c.endpoints.updateUser.Path()),
			htmx.Swap("none"),
			h.Class("space-y-6"),

			h.Div(
				h.Class("space-y-2"),
				h.Label(h.Class("block text-sm font-medium"), g.Text("Projektove token")),
				h.Input(
					h.Type("text"),
					h.Name("projektove_token"),
					h.Value(profile.ProjektoveToken),
					h.Class("w-full px-3 py-2 border rounded"),
				),
			),

			h.Div(
				h.ID("models-list"),
				h.Class("space-y-2"),
				h.Label(h.Class("block text-sm font-medium"), g.Text("LLM models")),
				g.Group(modelRows),
			),

			c.SaveButton(h.Type("submit")),
		),

		h.Form(
			h.ID("add-model-form"),
			h.Method("post"),
			htmx.Post(c.endpoints.addLLMModel.Path()),
			htmx.Target("#models-list"),
			htmx.Swap("beforeend"),
			htmx.On("htmx:after:request", "this.reset()"),
			h.Class("grid grid-rows-4 lg:grid-cols-[1fr_1fr_1fr_auto] gap-2 mt-4"),
			h.Select(h.Name("provider"), h.Placeholder("provider"), h.Class("px-2 py-1 border rounded"), h.Required(), h.Option(h.Value("googleai"), g.Text("google"))),
			h.Input(h.Type("text"), h.Name("model"), h.Placeholder("model"), h.Class("px-2 py-1 border rounded"), h.Required()),
			h.Input(h.Type("text"), h.Name("token"), h.Placeholder("token"), h.Class("px-2 py-1 border rounded"), h.Required()),
			c.AddButton("Add model", h.Type("submit")),
		),

		h.Hr(h.Class("my-8")),

		h.Div(
			h.Class("space-y-2"),
			h.H2(h.Class("text-xl font-semibold"), g.Text("Contexts")),
			h.Div(h.ID("contexts-list"), h.Class("space-y-2"), g.Group(c.contextRows(profile.Contexts))),
			h.Form(
				h.ID("add-context-form"),
				h.Method("post"),
				htmx.Post(c.endpoints.addContext.Path()),
				htmx.Target("#contexts-list"),
				htmx.Swap("beforeend"),
				htmx.On("htmx:after:request", "this.reset()"),
				h.Class("space-y-2"),
				h.Input(h.Type("text"), h.Name("name"), h.Placeholder("name"), h.Class("w-full px-2 py-1 border rounded"), h.Required()),
				h.Textarea(
					h.Name("context"),
					h.Placeholder("context"),
					h.Rows("5"),
					h.Class("w-full px-2 py-1 border rounded"),
					h.Required(),
				),
				c.AddButton("Add context", h.Type("submit")),
			),
		),
	)
}

func (c components) contextRows(contexts []ContextView) []g.Node {
	rows := []g.Node{}
	for _, cx := range contexts {
		rows = append(rows, c.ContextRow(cx))
	}
	return rows
}

func (c components) NewPromptPage(contexts []ContextView, models []LLMModel, validationErrors []string) g.Node {
	nodes := []g.Node{}

	if len(validationErrors) > 0 {
		items := []g.Node{}
		for _, m := range validationErrors {
			items = append(items, h.Li(g.Text(m)))
		}
		nodes = append(nodes,
			h.Div(
				h.Class("border border-red-300 bg-red-50 text-red-800 rounded px-4 py-3 max-w-2xl"),
				h.H2(h.Class("font-semibold mb-1"), g.Text("Prompt could not be created")),
				h.Ul(g.Group(items)),
			),
		)
	}

	modelOpts := []g.Node{}
	for _, m := range models {
		value := string(m.Provider) + "|" + m.Model
		label := string(m.Provider) + " / " + m.Model
		modelOpts = append(modelOpts, h.Option(h.Value(value), g.Text(label)))
	}

	contextOpts := []g.Node{}
	for _, cx := range contexts {
		contextOpts = append(contextOpts, h.Option(h.Value(cx.ID), g.Text(cx.Name)))
	}

	nodes = append(nodes,
		h.H1(h.Class("text-2xl font-bold mb-6"), g.Text("New prompt")),

		h.Form(
			h.Method("post"),
			h.Action(c.endpoints.createPrompt.Path()),
			h.EncType("multipart/form-data"),
			h.Class("space-y-6 max-w-2xl"),

			h.Div(
				h.Class("space-y-2"),
				h.Label(h.Class("block text-sm font-medium"), g.Text("LLM model")),
				h.Select(
					h.Name("model"),
					h.Required(),
					h.Class("w-full px-3 py-2 border rounded"),
					g.Group(modelOpts),
				),
			),

			h.Div(
				h.Class("space-y-2"),
				h.Label(h.Class("block text-sm font-medium"), g.Text("Context")),
				h.Select(
					h.Name("context_id"),
					h.Required(),
					h.Class("w-full px-3 py-2 border rounded"),
					g.Group(contextOpts),
				),
			),

			h.Div(
				h.Class("space-y-2"),
				h.Label(h.Class("block text-sm font-medium"), g.Text("Meeting notes")),
				h.Input(
					h.Type("file"),
					h.Name("meeting"),
					h.Accept(".txt,.md"),
					h.Required(),
					h.Class("w-full px-3 py-2 border rounded"),
				),
			),

			c.CreateButton(h.Type("submit")),
		),
	)

	return h.Div(g.Group(nodes))
}

func (c components) NewBatchPage(validationErrors []string) g.Node {
	nodes := []g.Node{}

	if len(validationErrors) > 0 {
		items := []g.Node{}
		for _, m := range validationErrors {
			items = append(items, h.Li(g.Text(m)))
		}
		nodes = append(nodes,
			h.Div(
				h.Class("border border-red-300 bg-red-50 text-red-800 rounded px-4 py-3 max-w-2xl"),
				h.H2(h.Class("font-semibold mb-1"), g.Text("The table could not be uploaded")),
				h.Ul(g.Group(items)),
			),
		)
	}

	nodes = append(nodes,
		h.H1(h.Class("text-2xl font-bold mb-6"), g.Text("New batch")),
		h.Form(
			h.Method("post"),
			h.Action(c.endpoints.createBatch.Path()),
			h.EncType("multipart/form-data"),
			h.Class("space-y-6 max-w-2xl"),
			h.Div(
				h.Class("space-y-2"),
				h.Label(h.Class("block text-sm font-medium"), g.Text("CSV table")),
				h.Input(
					h.Type("file"),
					h.Name("csv"),
					h.Accept(".csv,text/csv"),
					h.Required(),
					h.Class("w-full px-3 py-2 border rounded"),
				),
				h.P(
					h.Class("text-sm text-gray-500"),
					g.Text(`The table must contain columns "Subject", "Project", "Start date" and "Due date". Optional columns: "Description", "Assignee".`),
				),
			),
			c.CreateButton(h.Type("submit")),
		),
	)

	return h.Div(g.Group(nodes))
}

func dateValue(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

func (c components) PromptFragment(view PromptView, projects []ProjectOptionView, users []ProjektoveUser) g.Node {
	if view.Status.IsPending() {
		return h.Div(
			h.ID("prompt-view"),
			htmx.Get("/prompts/"+view.ID),
			htmx.Trigger("every 5s"),
			htmx.Swap("outerHTML"),
			h.Class("space-y-6 max-w-3xl"),
			h.H1(h.Class("text-2xl font-bold mb-2"), g.Text("Prompt "+view.ID)),
			h.Div(
				h.Class("text-sm text-gray-500"),
				g.Text("Context: "+view.ContextName),
			),
			h.Div(
				h.Class("flex items-center gap-3 text-gray-500 py-8"),
				solid.ArrowPath(h.Class("h-5 w-5 animate-spin")),
				g.Text("Processing..."),
			),
		)
	}

	issueCards := []g.Node{}
	for _, iss := range view.Issues {
		issueCards = append(issueCards, c.IssueCard(iss, projects, users))
	}

	nodes := []g.Node{
		h.Div(
			h.Class("space-y-6 max-w-3xl"),
			h.H1(h.Class("text-2xl font-bold mb-2"), g.Text("Prompt "+view.ID)),
			h.Div(
				h.Class("text-sm text-gray-500"),
				g.Text("Context: "+view.ContextName),
			),
		),
	}

	if view.Error != "" {
		nodes = append(nodes,
			h.Div(
				h.Class("border border-red-300 bg-red-50 text-red-800 rounded px-4 py-3"),
				g.Text(view.Error),
			),
		)
	}

	nodes = append(nodes,
		h.Div(
			h.Class("space-y-4"),
			h.Div(
				h.Class("space-y-1"),
				h.H2(h.Class("text-lg font-semibold"), g.Text("Prompt")),
				h.Textarea(
					h.Class("font-mono space-y-1 w-full bg-gray-100 border rounded p-2 text-gray-600"),
					g.Text(view.Prompt),
					h.Disabled(),
					h.Rows("5"),
				),
			),
			h.Div(
				h.Class("space-y-1"),
				h.H2(h.Class("text-lg font-semibold"), g.Text("Result")),
				h.Textarea(
					h.Class("font-mono space-y-1 w-full bg-gray-100 border rounded p-2 text-gray-600"),
					g.Text(view.Result),
					h.Disabled(),
					h.Rows("5"),
				),
			),
		),
	)

	hasPending := false
	for _, iss := range view.Issues {
		if iss.Editable {
			hasPending = true
			break
		}
	}

	if hasPending {
		nodes = append(nodes,
			h.Div(
				h.ID("submit-all-row"),
				h.Class("flex items-center gap-3"),
				c.SubmitButton(
					h.Type("button"),
					g.Attr("onclick", "submitAll()"),
				),
				h.Span(h.Class("text-sm text-gray-500"), g.Text("Submit all pending issues")),
			),
		)
	}

	nodes = append(nodes, h.Div(h.Class("space-y-4"), g.Group(issueCards)))

	return h.Div(h.ID("prompt-view"), h.Class("flex flex-col gap-4"), g.Group(nodes))
}

func (c components) BatchFragment(view BatchView, projects []ProjectOptionView, users []ProjektoveUser) g.Node {
	issueCards := []g.Node{}
	for _, iss := range view.Issues {
		issueCards = append(issueCards, c.IssueCard(iss, projects, users))
	}

	nodes := []g.Node{
		h.Div(
			h.Class("space-y-6 max-w-3xl"),
			h.H1(h.Class("text-2xl font-bold mb-2"), g.Text("Batch "+view.ID)),
		),

		h.Div(
			h.Class("space-y-1"),
			h.H2(h.Class("text-lg font-semibold"), g.Text("Result")),
			h.Textarea(
				h.Class("font-mono space-y-1 w-full bg-gray-100 border rounded p-2 text-gray-600"),
				g.Text(view.FileContent),
				h.Disabled(),
				h.Rows("5"),
			),
		),
	}

	hasPending := false
	for _, iss := range view.Issues {
		if iss.Editable {
			hasPending = true
			break
		}
	}

	if hasPending {
		nodes = append(nodes,
			h.Div(
				h.ID("submit-all-row"),
				h.Class("flex items-center gap-3"),
				c.SubmitButton(
					h.Type("button"),
					g.Attr("onclick", "submitAll()"),
				),
				h.Span(h.Class("text-sm text-gray-500"), g.Text("Submit all pending issues")),
			),
		)
	}

	nodes = append(nodes, h.Div(h.Class("space-y-4"), g.Group(issueCards)))

	return h.Div(h.ID("batch-view"), h.Class("flex flex-col gap-4"), g.Group(nodes))
}

func (c components) IssueCard(iss IssueView, projects []ProjectOptionView, users []ProjektoveUser) g.Node {
	cardID := "issue-" + iss.ID

	if !iss.Editable {
		if iss.Status == IssueStatusDeleted {
			return h.Div(
				h.ID(cardID),
				h.Class("border rounded p-4 flex items-start justify-between"),
				h.Div(
					h.Div(h.Class("font-medium"), g.Text(iss.Subject)),
					h.Div(h.Class("text-sm text-gray-500"), g.Text(iss.Description)),
				),
				h.Div(
					h.Class("flex items-center gap-2 text-gray-500 text-sm"),
					solid.Trash(h.Class("h-5 w-5")),
					g.Text("Deleted"),
				),
			)
		}

		status := g.Group([]g.Node{
			solid.CheckCircle(h.Class("h-5 w-5 text-green-600")),
			g.Text("Submitted"),
		})
		meta := h.Span()
		if iss.ProjektoveID != nil {
			meta = h.A(h.Class("underline"), h.Target("_blank"), h.Href(fmt.Sprintf(c.projektoveIssueEndpoint, *iss.ProjektoveID)), g.Text("Projektove #"+strconv.Itoa(*iss.ProjektoveID)))
		}
		return h.Div(
			h.ID(cardID),
			h.Class("border rounded p-4 flex items-start justify-between"),
			h.Div(
				h.Div(h.Class("font-medium"), g.Text(iss.Subject)),
				h.Div(h.Class("text-sm text-gray-500"), g.Text(iss.Description)),
			),
			h.Div(
				h.Class("flex items-center gap-2 text-green-700 text-sm"),
				status,
				meta,
			),
		)
	}

	projectOpts := []g.Node{}
	if iss.ProjectID == 0 {
		projectOpts = append(projectOpts, h.Option(h.Value(""), h.Disabled(), h.Selected(), g.Text("Select project")))
	} else {
		projectOpts = append(projectOpts, h.Option(h.Value(""), h.Disabled(), g.Text("Select project")))
	}
	for _, p := range projects {
		opts := []g.Node{h.Value(strconv.Itoa(p.ID)), g.Text(p.Name)}
		if p.ID == iss.ProjectID {
			opts = append([]g.Node{h.Selected()}, opts...)
		}
		projectOpts = append(projectOpts, h.Option(opts...))
	}

	userOpts := []g.Node{}
	if iss.AssignedToID == 0 {
		userOpts = append(userOpts, h.Option(h.Value(""), h.Disabled(), h.Selected(), g.Text("Select user")))
	} else {
		userOpts = append(userOpts, h.Option(h.Value(""), h.Disabled(), g.Text("Select user")))
	}
	for _, u := range users {
		opts := []g.Node{h.Value(strconv.Itoa(u.ID)), g.Text(u.Name)}
		if u.ID == iss.AssignedToID {
			opts = append([]g.Node{h.Selected()}, opts...)
		}
		userOpts = append(userOpts, h.Option(opts...))
	}

	updatePath := strings.Replace(c.endpoints.updateIssue.Path(), "{id}", iss.ID, 1)
	submitPath := strings.Replace(c.endpoints.submitIssue.Path(), "{id}", iss.ID, 1)
	deletePath := strings.Replace(c.endpoints.deleteIssue.Path(), "{id}", iss.ID, 1)
	indicator := "#submit-indicator-" + iss.ID

	return h.Form(
		h.ID(cardID),
		h.Class("border rounded p-4 space-y-3"),
		g.If(iss.Error != "", h.Div(h.Class("border border-red-300 bg-red-50 text-red-800 rounded px-3 py-2 text-sm"), g.Text(iss.Error))),
		h.Input(h.Type("text"), h.Name("subject"), h.Value(iss.Subject), h.Required(), h.Class("w-full px-3 py-2 border rounded font-medium")),
		h.Textarea(h.Name("description"), h.Rows("2"), h.Class("w-full px-3 py-2 border rounded"), g.Text(iss.Description)),

		h.Div(
			h.Class("grid grid-cols-2 gap-3"),
			h.Div(
				h.Class("space-y-1"),
				h.Label(h.Class("block text-sm text-gray-500"), g.Text("Project")),
				h.Select(h.Name("project_id"), h.Class("w-full px-3 py-2 border rounded"), g.Group(projectOpts), h.Required()),
			),
			h.Div(
				h.Class("space-y-1"),
				h.Label(h.Class("block text-sm text-gray-500"), g.Text("Assignee")),
				h.Select(h.Name("assigned_to_id"), h.Class("w-full px-3 py-2 border rounded"), g.Group(userOpts), h.Required()),
			),
			h.Div(
				h.Class("space-y-1"),
				h.Label(h.Class("block text-sm text-gray-500"), g.Text("Start date")),
				h.Input(h.Type("date"), h.Name("start_date"), h.Value(dateValue(iss.StartDate)), h.Class("w-full px-3 py-2 border rounded"), h.Required()),
			),
			h.Div(
				h.Class("space-y-1"),
				h.Label(h.Class("block text-sm text-gray-500"), g.Text("Due date")),
				h.Input(h.Type("date"), h.Name("due_date"), h.Value(dateValue(iss.DueDate)), h.Class("w-full px-3 py-2 border rounded"), h.Required()),
			),
		),

		h.Div(
			h.Class("flex items-center gap-2"),
			c.SaveButton(
				h.Type("button"),
				htmx.Put(updatePath),
				htmx.Include("closest form"),
				htmx.Target("#"+cardID),
				htmx.Swap("outerHTML"),
			),
			c.SubmitButton(
				h.Type("submit"),
				g.Attr("data-submit-issue", ""),
				htmx.Validate("true"),
				htmx.Post(submitPath),
				htmx.Include("closest form"),
				htmx.Target("#"+cardID),
				htmx.Swap("outerHTML"),
				htmx.Indicator(indicator),
			),
			h.Span(
				h.ID("submit-indicator-"+iss.ID),
				h.Class("htmx-indicator inline-flex items-center gap-1 text-sm text-gray-500"),
				solid.ArrowPath(h.Class("h-4 w-4 animate-spin")),
				g.Text("Submitting..."),
			),
			h.Div(h.Class("flex-1")),
			c.DeleteButton(
				h.Type("button"),
				htmx.Delete(deletePath),
				htmx.Target("#"+cardID),
				htmx.Swap("outerHTML"),
				htmx.Confirm("Delete this issue?"),
			),
		),
	)
}

func (c components) ContextRow(cx ContextView) g.Node {
	return h.Div(
		h.Class("context-row flex justify-between items-center border rounded px-3 py-2"),
		h.Div(
			h.Div(h.Class("font-medium"), g.Text(cx.Name)),
			h.Div(h.Class("text-sm text-gray-500"), g.Text(cx.Context)),
		),
		c.DeleteButton(
			h.Type("button"),
			htmx.Delete(strings.Replace(c.endpoints.deleteContext.Path(), "{id}", cx.ID, 1)),
			htmx.Target("closest .context-row"),
			htmx.Swap("outerHTML swap:0.2s"),
			htmx.Confirm("Delete this context?"),
		),
	)
}

func (c components) ModelRow(m LLMModelView) g.Node {
	return h.Div(
		h.Class("model-row flex gap-2 items-center border rounded px-3 py-2 overflow-auto"),
		h.Input(h.Type("text"), h.Name("model_provider"), h.Value(string(m.Provider)), h.Class("flex-1 px-2 py-1 border rounded")),
		h.Input(h.Type("text"), h.Name("model_name"), h.Value(m.Model), h.Class("flex-1 px-2 py-1 border rounded")),
		h.Input(h.Type("text"), h.Name("model_token"), h.Value(m.Token), h.Class("flex-1 px-2 py-1 border rounded")),
		c.DeleteButton(h.Type("button"), g.Attr("onclick", "this.closest('.model-row').remove()")),
	)
}

// button is the base button component. Concrete buttons should use it
// and supply their own colors, sizes and icons.
func (c components) button(class string, opts ...g.Node) g.Node {
	return h.Button(
		append([]g.Node{h.Class(class)}, opts...)...,
	)
}

func (c components) SaveButton(opts ...g.Node) g.Node {
	return c.button(
		baseButtonClass+"bg-gray-800 hover:bg-gray-900 text-white px-4 py-2 disabled:bg-gray-400 disabled:hover:bg-gray-400",
		append(opts, solid.Check(h.Class("h-4 w-4")), g.Text("Save"))...,
	)
}

func (c components) AddButton(label string, opts ...g.Node) g.Node {
	return c.button(
		baseButtonClass+"bg-gray-800 hover:bg-gray-900 text-white px-3 py-1 disabled:bg-gray-400 disabled:hover:bg-gray-400",
		append(opts, solid.Plus(h.Class("h-4 w-4")), g.Text(label))...,
	)
}

func (c components) DeleteButton(opts ...g.Node) g.Node {
	return c.button(
		baseButtonClass+"bg-red-700 hover:bg-red-800 text-white px-2 py-1 disabled:bg-red-300 disabled:hover:bg-red-300",
		append(opts, solid.Trash(h.Class("h-4 w-4")), g.Text("Remove"))...,
	)
}

func (c components) CreateButton(opts ...g.Node) g.Node {
	return c.button(
		baseButtonClass+"bg-gray-800 hover:bg-gray-900 text-white px-4 py-2 disabled:bg-gray-400 disabled:hover:bg-gray-400",
		append(opts, solid.Sparkles(h.Class("h-4 w-4")), g.Text("Create issues"))...,
	)
}

func (c components) SubmitButton(opts ...g.Node) g.Node {
	return c.button(
		baseButtonClass+"bg-gray-800 hover:bg-gray-900 text-white px-4 py-2 disabled:bg-gray-400 disabled:hover:bg-gray-400",
		append(opts, solid.PaperAirplane(h.Class("h-4 w-4")), g.Text("Submit"))...,
	)
}

func (a api) notImplemented(w http.ResponseWriter, message string) {
	WriteError(w, message, http.StatusNotImplemented, nil)
}
