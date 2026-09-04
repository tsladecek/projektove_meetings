package projektovemeeting

import (
	"embed"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

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

func NewHandler(auth Auth, baseURL, cookieName string, controller Controller) http.Handler {
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

		// pages
		user:      end(http.MethodGet, "/user"),
		prompts:   end(http.MethodGet, "/prompts/"),
		newPrompt: end(http.MethodGet, "/prompts/new"),
		prompt:    end(http.MethodGet, "/prompts/{id}"),

		// api

		updateUser:    endAPI(http.MethodPut, "/user"),
		addContext:    endAPI(http.MethodPost, "/contexts"),
		addLLMModel:   endAPI(http.MethodPost, "/models"),
		deleteContext: endAPI(http.MethodDelete, "/contexts/{id}"),

		listContexts: endAPI(http.MethodGet, "/contexts"),
		createPrompt: endAPI(http.MethodPost, "/prompts"),

		updateIssue: endAPI(http.MethodPut, "/issues"),
		submitIssue: endAPI(http.MethodPost, "/issues/submit"),
	}

	c := components{endpoints: e}
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

		// api
		{endpoint: e.updateUser, handler: a.updateUser()},
		{endpoint: e.addContext, handler: a.addContext()},
		{endpoint: e.deleteContext, handler: a.deleteContext()},
		{endpoint: e.addLLMModel, handler: a.addLLMModel()},
		{endpoint: e.listContexts, handler: a.listContexts()},
		{endpoint: e.createPrompt, handler: a.createPrompt()},
		{endpoint: e.updateIssue, handler: a.updateIssue()},
		{endpoint: e.submitIssue, handler: a.submitIssue()},
	} {
		m.Handle(eh.endpoint.Pattern(), auth.Middleware(eh.handler))
	}

	auth.RegisterRoutes(m)

	handler := MiddlewareLogging(m)
	return handler
}

func (a api) root() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.components.Page(h.Div(g.Text("root"))).Render(w)
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

func (a api) prompts() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.components.Page(a.components.PageStub("Prompts", "Prompts list not implemented yet.")).Render(w)
	}
}

func (a api) newPrompt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.components.Page(a.components.PageStub("New prompt", "New prompt page not implemented yet.")).Render(w)
	}
}

func (a api) prompt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		a.components.Page(a.components.PageStub("Prompt "+id, "Prompt detail not implemented yet.")).Render(w)
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

		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil {
			WriteError(w, "invalid context id", http.StatusBadRequest, err)
			return
		}

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
		a.notImplemented(w, "create prompt not implemented")
	}
}

func (a api) updateIssue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.notImplemented(w, "update issue not implemented")
	}
}

func (a api) submitIssue() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.notImplemented(w, "submit issue not implemented")
	}
}

type endpoints struct {
	root   Endpoint
	static Endpoint

	// pages
	user      Endpoint
	prompts   Endpoint
	newPrompt Endpoint
	prompt    Endpoint

	// api - should have /api prefix
	updateUser    Endpoint
	addContext    Endpoint
	deleteContext Endpoint
	addLLMModel   Endpoint

	listContexts Endpoint
	createPrompt Endpoint

	updateIssue Endpoint
	submitIssue Endpoint
}

type components struct {
	endpoints endpoints
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
}

func (c components) Sidebar() g.Node {
	links := []navLink{
		{label: "User", href: c.endpoints.user.Path()},
		{label: "New prompt", href: c.endpoints.newPrompt.Path()},
		{label: "Prompts", href: c.endpoints.prompts.Path()},
	}

	navItems := []g.Node{}
	for _, l := range links {
		navItems = append(navItems, h.A(
			h.Href(l.href),
			h.Class("block px-4 py-2 rounded hover:bg-gray-700"),
			g.Text(l.label),
		))
	}

	return h.Aside(
		h.Class("w-56 bg-gray-800 text-white flex flex-col p-4"),
		h.Nav(
			h.Class("space-y-1"),
			g.Group(navItems),
		),
	)
}

func (c components) PageStub(title, message string) g.Node {
	return h.Div(
		h.H1(h.Class("text-2xl font-bold mb-4"), g.Text(title)),
		h.P(g.Text(message)),
	)
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
			h.Class("grid grid-cols-[1fr_1fr_1fr_auto] gap-2 mt-4"),
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

func (c components) ContextRow(cx ContextView) g.Node {
	return h.Div(
		h.Class("context-row flex justify-between items-center border rounded px-3 py-2"),
		h.Div(
			h.Div(h.Class("font-medium"), g.Text(cx.Name)),
			h.Div(h.Class("text-sm text-gray-500"), g.Text(cx.Context)),
		),
		c.DeleteButton(
			h.Type("button"),
			htmx.Delete(strings.Replace(c.endpoints.deleteContext.Path(), "{id}", strconv.Itoa(cx.ID), 1)),
			htmx.Target("closest .context-row"),
			htmx.Swap("outerHTML swap:0.2s"),
			htmx.Confirm("Delete this context?"),
		),
	)
}

func (c components) ModelRow(m LLMModelView) g.Node {
	return h.Div(
		h.Class("model-row flex gap-2 items-center border rounded px-3 py-2"),
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

func (a api) notImplemented(w http.ResponseWriter, message string) {
	WriteError(w, message, http.StatusNotImplemented, nil)
}
