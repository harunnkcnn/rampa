// Package app is Rampa's HTTP application: routing, auth, rendering and the
// extension API that commercial add-ons build on.
package app

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/harunnkcnn/rampa/store"
	"github.com/harunnkcnn/rampa/web"
)

// Version is set at build time with -ldflags "-X github.com/harunnkcnn/rampa/app.Version=v1.0.0".
var Version = "dev"

// Config is read from flags and environment by cmd/rampa.
type Config struct {
	Addr         string
	DBPath       string
	SecureCookie bool          // set when served over HTTPS
	SessionTTL   time.Duration // default 12h
	Edition      string        // shown in the sidebar, e.g. "Açık sürüm"
}

// Extension adds routes, pages and schema to the core. The core never
// imports an extension; a separate main package wires them together.
type Extension interface {
	Name() string
	Migrations() []store.Migration
	Register(s *Server) error
}

// NavItem is a sidebar link.
type NavItem struct {
	Label  string
	Href   string
	Group  string // "" for the main group
	Prefix string // active when the path starts with this; defaults to Href
	Order  int
	Admin  bool // shown to administrators only
}

// Server holds the application's dependencies.
type Server struct {
	cfg      Config
	Store    *store.Store
	Log      *slog.Logger
	render   *Renderer
	term     *Renderer
	mux      *http.ServeMux
	nav      []NavItem
	limiter  *loginLimiter
	exts     []Extension
	afterNav []func(r *http.Request) []Notice
	panels   []ShipmentPanelFunc
	styles   []string
}

// Notice is a banner shown above page content (e.g. trial remaining).
type Notice struct {
	Kind string // "info", "warn"
	Text string
	Href string
	Link string
}

// New builds the server, runs migrations and registers routes.
func New(ctx context.Context, cfg Config, st *store.Store, log *slog.Logger, exts ...Extension) (*Server, error) {
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.Edition == "" {
		cfg.Edition = "Açık sürüm"
	}
	r, err := NewRenderer(web.FS)
	if err != nil {
		return nil, err
	}
	term, err := newRendererAt(web.FS, "templates/terminal/pages/*.html", "templates/terminal/layout.html")
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:     cfg,
		Store:   st,
		Log:     log,
		render:  r,
		term:    term,
		mux:     http.NewServeMux(),
		limiter: newLoginLimiter(5, 15*time.Minute),
		exts:    exts,
	}

	migrations := append([]store.Migration{}, store.CoreMigrations...)
	seen := map[string]bool{}
	for _, m := range store.CoreMigrations {
		seen[m.Name] = true
	}
	for _, e := range exts {
		for _, m := range e.Migrations() {
			if seen[m.Name] {
				return nil, fmt.Errorf("extension %s: duplicate migration %s", e.Name(), m.Name)
			}
			seen[m.Name] = true
			migrations = append(migrations, m)
		}
	}
	if err := st.Migrate(ctx, migrations); err != nil {
		return nil, err
	}

	s.routes()
	for _, e := range exts {
		if err := e.Register(s); err != nil {
			return nil, fmt.Errorf("extension %s: %w", e.Name(), err)
		}
	}
	return s, nil
}

// Config returns the active configuration.
func (s *Server) Config() Config { return s.cfg }

// Handle registers an authenticated route, e.g. "GET /pro/reports".
func (s *Server) Handle(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireUser(h))
}

// HandlePublic registers a route that does not require a session.
func (s *Server) HandlePublic(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, h)
}

// AddNav adds a sidebar link.
func (s *Server) AddNav(items ...NavItem) {
	s.nav = append(s.nav, items...)
}

// AddStylesheet links an extra stylesheet from every page.
func (s *Server) AddStylesheet(href string) { s.styles = append(s.styles, href) }

// AddNotices registers a provider of page banners.
func (s *Server) AddNotices(fn func(r *http.Request) []Notice) {
	s.afterNav = append(s.afterNav, fn)
}

// AddPages parses extension templates (each file one page) against the
// core layout. extra funcs are available only to these pages.
func (s *Server) AddPages(fsys fs.FS, glob string, extra template.FuncMap) error {
	return s.render.AddPages(fsys, glob, extra)
}

// Handler returns the root handler with middleware applied.
func (s *Server) Handler() http.Handler {
	var h http.Handler = s.mux
	h = s.withScopeCache(h)
	h = s.checkCSRF(h)
	h = s.loadSession(h)
	h = securityHeaders(h)
	h = s.logRequests(h)
	h = s.recoverPanics(h)
	return h
}

func (s *Server) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.Log.Error("panic", "path", r.URL.Path, "err", v)
				http.Error(w, "Beklenmeyen bir hata oluştu.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		s.Log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"dur", time.Since(start).Round(time.Millisecond))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; "+
			"script-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// ServerError logs err and responds with a generic 500.
func (s *Server) ServerError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return
	}
	s.Log.Error("handler", "path", r.URL.Path, "err", err)
	http.Error(w, "Beklenmeyen bir hata oluştu. Ayrıntılar sunucu kaydında.", http.StatusInternalServerError)
}

// NotFound renders the 404 page.
func (s *Server) NotFound(w http.ResponseWriter, r *http.Request) {
	p := s.Page(r, "Bulunamadı", "")
	s.HTML(w, r, http.StatusNotFound, "notfound", p)
}

// IsHTMX reports whether the request came from htmx.
func IsHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// Redirect sends a 303, or HX-Redirect for htmx requests.
func Redirect(w http.ResponseWriter, r *http.Request, url string) {
	if IsHTMX(r) {
		w.Header().Set("HX-Redirect", url)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, url, http.StatusSeeOther)
}
