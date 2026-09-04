package projektovemeeting

import (
	"embed"
	"net/http"
	"net/url"
	"path"

	g "maragu.dev/gomponents"
	co "maragu.dev/gomponents/components"
	h "maragu.dev/gomponents/html"
)

//go:embed static/*
var staticFS embed.FS

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

		updateUser:  endAPI(http.MethodPut, "/user"),
		addContext:  endAPI(http.MethodPost, "/contexts"),
		addLLMModel: endAPI(http.MethodPost, "/models"),

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
		a.components.Page(a.components.PageStub("User", "User page not implemented yet.")).Render(w)
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
		a.notImplemented(w, "update user not implemented")
	}
}

func (a api) addContext() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.notImplemented(w, "add context not implemented")
	}
}

func (a api) addLLMModel() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a.notImplemented(w, "add llm model not implemented")
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
	updateUser  Endpoint
	addContext  Endpoint
	addLLMModel Endpoint

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

func (a api) notImplemented(w http.ResponseWriter, message string) {
	WriteError(w, message, http.StatusNotImplemented, nil)
}
