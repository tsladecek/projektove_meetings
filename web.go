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
					h.Class("w-screen"),
					body,
				),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "popper.min.js"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "tippy-bundle.umd.min.js"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "toastify.js"))),
				h.Script(h.Src(path.Join(c.endpoints.static.Path(), "js", "general.js"))),
			},
		},
	)
}
