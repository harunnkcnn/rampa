package app

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/harunnkcnn/rampa/store"
)

// Renderer holds one template set per page, each sharing the core layout.
type Renderer struct {
	base  *template.Template
	pages map[string]*template.Template
}

// NewRenderer parses the core layout, partials and pages from fsys.
func NewRenderer(fsys fs.FS) (*Renderer, error) {
	return newRendererAt(fsys, "templates/pages/*.html", "templates/layout.html", "templates/partials/*.html")
}

func newRendererAt(fsys fs.FS, pages string, base ...string) (*Renderer, error) {
	t, err := template.New("base").Funcs(Funcs()).ParseFS(fsys, base...)
	if err != nil {
		return nil, err
	}
	r := &Renderer{base: t, pages: map[string]*template.Template{}}
	return r, r.AddPages(fsys, pages, nil)
}

// AddPages parses each file matching glob as its own page.
func (r *Renderer) AddPages(fsys fs.FS, glob string, extra template.FuncMap) error {
	files, err := fs.Glob(fsys, glob)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no templates match %s", glob)
	}
	for _, f := range files {
		name := strings.TrimSuffix(path.Base(f), ".html")
		if _, dup := r.pages[name]; dup {
			return fmt.Errorf("duplicate page template %q", name)
		}
		t, err := r.base.Clone()
		if err != nil {
			return err
		}
		if extra != nil {
			t = t.Funcs(extra)
		}
		if t, err = t.ParseFS(fsys, f); err != nil {
			return err
		}
		r.pages[name] = t
	}
	return nil
}

func (r *Renderer) exec(w http.ResponseWriter, status int, page, block string, data any) error {
	t, ok := r.pages[page]
	if !ok {
		return fmt.Errorf("unknown page %q", page)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, block, data); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, err := buf.WriteTo(w)
	return err
}

// Page is the data passed to every template.
type Page struct {
	Title   string
	Active  string
	User    *store.User
	CSRF    string
	Nav     []NavGroup
	Notices []Notice
	Flash   *Flash
	Company string
	Edition string
	Version string
	Errors  map[string]string
	Form    url.Values
	Data    any
	Path    string
	Styles  []string
	Scope   Scope
}

// NavGroup is a titled block of sidebar links.
type NavGroup struct {
	Title string
	Items []NavLink
}

// NavLink is a rendered sidebar link.
type NavLink struct {
	Label  string
	Href   string
	Active bool
}

// Err returns the validation message for a field.
func (p *Page) Err(field string) string { return p.Errors[field] }

// Val returns the submitted form value for a field.
func (p *Page) Val(field string) string { return p.Form.Get(field) }

// Page builds the common page data for r.
func (s *Server) Page(r *http.Request, title, active string) *Page {
	p := &Page{
		Title:   title,
		Active:  active,
		Edition: s.cfg.Edition,
		Version: Version,
		Errors:  map[string]string{},
		Form:    url.Values{},
		Path:    r.URL.Path,
		Styles:  s.styles,
	}
	if ss := SessionFrom(r.Context()); ss != nil {
		u := ss.User
		p.User = &u
		p.CSRF = ss.CSRF
		p.Nav = s.navFor(r.URL.Path, u.IsAdmin)
		p.Scope, _ = s.ScopeOf(r)
		for _, fn := range s.afterNav {
			p.Notices = append(p.Notices, fn(r)...)
		}
	}
	p.Company, _ = s.Store.Setting(r.Context(), "company_name", "")
	return p
}

func (s *Server) navFor(cur string, admin bool) []NavGroup {
	var items []NavItem
	for _, it := range s.nav {
		if !it.Admin || admin {
			items = append(items, it)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Order < items[j].Order })
	var groups []NavGroup
	idx := map[string]int{}
	best := ""
	for _, it := range items {
		pfx := it.Prefix
		if pfx == "" {
			pfx = it.Href
		}
		if (cur == pfx || (pfx != "/" && strings.HasPrefix(cur, pfx))) && len(pfx) > len(best) {
			best = pfx
		}
	}
	for _, it := range items {
		pfx := it.Prefix
		if pfx == "" {
			pfx = it.Href
		}
		i, ok := idx[it.Group]
		if !ok {
			groups = append(groups, NavGroup{Title: it.Group})
			i = len(groups) - 1
			idx[it.Group] = i
		}
		groups[i].Items = append(groups[i].Items, NavLink{Label: it.Label, Href: it.Href, Active: pfx == best})
	}
	return groups
}

// HTML renders a full page inside the layout, consuming any flash message.
func (s *Server) HTML(w http.ResponseWriter, r *http.Request, status int, page string, p *Page) {
	if p.Flash == nil {
		p.Flash = popFlash(w, r)
	}
	if err := s.render.exec(w, status, page, "layout", p); err != nil {
		s.ServerError(w, r, err)
	}
}

// Partial renders one named block of a page (for htmx swaps).
func (s *Server) Partial(w http.ResponseWriter, r *http.Request, status int, page, block string, data any) {
	if err := s.render.exec(w, status, page, block, data); err != nil {
		s.ServerError(w, r, err)
	}
}

// Field is the data for the shared text-field template.
type Field struct {
	Name, Label, Hint, Mode, Value, Err string
	Required, Full                      bool
}

// Funcs are the template helpers available everywhere.
func Funcs() template.FuncMap {
	return template.FuncMap{
		"date": func(t time.Time) string { return t.Format("02.01.2006") },
		"datetime": func(t time.Time) string {
			return t.Format("02.01.2006 15:04")
		},
		"ptime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("02.01.2006 15:04")
		},
		"isoDate": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"num": formatFloat,
		"pct": func(v float64) string { return formatFloat(v, 0) },
		"signed": func(q store.Qty) string {
			if q > 0 {
				return "+" + q.String()
			}
			return q.String()
		},
		"neg":   func(q store.Qty) store.Qty { return -q },
		"add":   func(a, b int) int { return a + b },
		"clamp": func(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) },
		"mul":   func(a, b float64) float64 { return a * b },
		"query": func(pairs ...string) template.URL {
			v := url.Values{}
			for i := 0; i+1 < len(pairs); i += 2 {
				if pairs[i+1] != "" {
					v.Set(pairs[i], pairs[i+1])
				}
			}
			if len(v) == 0 {
				return ""
			}
			return template.URL("?" + v.Encode())
		},
		"itoa": func(n int64) string { return strconv.FormatInt(n, 10) },
		// binOnly turns "06/A-01-1" into "A-01-1".
		"binOnly": func(s string) string {
			if i := strings.LastIndex(s, "/"); i >= 0 {
				return s[i+1:]
			}
			return s
		},
		"fieldOf": func(p *Page, name, label, hint, mode string, required, full bool) Field {
			return Field{Name: name, Label: label, Hint: hint, Mode: mode, Required: required, Full: full,
				Value: p.Val(name), Err: p.Err(name)}
		},
		"mtable": func(rows []store.Movement, hideProduct bool) any {
			return struct {
				Rows        []store.Movement
				HideProduct bool
			}{rows, hideProduct}
		},
	}
}

// formatFloat formats with Turkish separators and at most dec decimals.
func formatFloat(v float64, dec int) string {
	s := strconv.FormatFloat(v, 'f', dec, 64)
	if dec > 0 {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	if frac != "" {
		b.WriteByte(',')
		b.WriteString(frac)
	}
	return b.String()
}
