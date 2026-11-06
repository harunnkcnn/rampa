package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/harunnkcnn/rampa/store"
)

const sessionCookie = "rampa_session"

type ctxKey int

const sessionKey ctxKey = 1

// SessionFrom returns the signed-in session, or nil.
func SessionFrom(ctx context.Context) *store.Session {
	ss, _ := ctx.Value(sessionKey).(*store.Session)
	return ss
}

// UserID returns the signed-in user's id, or 0.
func UserID(r *http.Request) int64 {
	if ss := SessionFrom(r.Context()); ss != nil {
		return ss.User.ID
	}
	return 0
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err == nil && c.Value != "" {
			ss, err := s.Store.GetSession(r.Context(), hashToken(c.Value))
			if err == nil {
				r = r.WithContext(context.WithValue(r.Context(), sessionKey, &ss))
			} else if !errors.Is(err, store.ErrNotFound) {
				s.ServerError(w, r, err)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) == nil {
			n, err := s.Store.CountUsers(r.Context())
			if err != nil {
				s.ServerError(w, r, err)
				return
			}
			if n == 0 {
				Redirect(w, r, "/setup")
				return
			}
			target := "/login"
			if r.Method == http.MethodGet && r.URL.Path != "/" {
				target += "?next=" + url.QueryEscape(r.URL.RequestURI())
			}
			Redirect(w, r, target)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkCSRF blocks cross-site writes. Browsers send Sec-Fetch-Site; older
// ones send Origin. Signed-in requests must also carry the session token.
func (s *Server) checkCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "Başka bir siteden gelen istek reddedildi.", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "Başka bir siteden gelen istek reddedildi.", http.StatusForbidden)
				return
			}
		}
		if ss := SessionFrom(r.Context()); ss != nil {
			tok := r.Header.Get("X-CSRF-Token")
			if tok == "" {
				tok = r.PostFormValue("_csrf")
			}
			if subtle.ConstantTimeCompare([]byte(tok), []byte(ss.CSRF)) != 1 {
				http.Error(w, "Oturum doğrulaması başarısız. Sayfayı yenileyip tekrar deneyin.", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	tok := randomToken(32)
	exp := s.Store.Now().Add(s.cfg.SessionTTL)
	if err := s.Store.CreateSession(r.Context(), hashToken(tok), randomToken(24), userID, exp); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   s.cfg.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	_ = s.Store.PurgeExpiredSessions(r.Context())
	return nil
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.cfg.SecureCookie, SameSite: http.SameSiteLaxMode})
}

// safeNext only allows local paths, preventing open redirects.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if SessionFrom(r.Context()) != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if n, err := s.Store.CountUsers(r.Context()); err == nil && n == 0 {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	p := s.Page(r, "Giriş", "")
	p.Form.Set("next", r.URL.Query().Get("next"))
	s.HTML(w, r, http.StatusOK, "login", p)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	p := s.Page(r, "Giriş", "")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form okunamadı.", http.StatusBadRequest)
		return
	}
	p.Form = r.PostForm
	ip := clientIP(r)
	if !s.limiter.allow(ip) {
		p.Errors["form"] = "Çok fazla hatalı deneme. 15 dakika sonra tekrar deneyin."
		s.HTML(w, r, http.StatusTooManyRequests, "login", p)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")

	u, err := s.Store.GetUserByUsername(r.Context(), username)
	hash := u.PasswordHash
	if errors.Is(err, store.ErrNotFound) {
		hash = dummyHash()
	} else if err != nil {
		s.ServerError(w, r, err)
		return
	}
	ok, _ := CheckPassword(hash, password)
	if !ok || u.ID == 0 {
		s.limiter.fail(ip)
		p.Errors["form"] = "Kullanıcı adı ya da parola hatalı."
		s.HTML(w, r, http.StatusUnauthorized, "login", p)
		return
	}
	s.limiter.reset(ip)
	if err := s.startSession(w, r, u.ID); err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if ss := SessionFrom(r.Context()); ss != nil {
		_ = s.Store.DeleteSession(r.Context(), ss.TokenHash)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupForm(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if n > 0 {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.HTML(w, r, http.StatusOK, "setup", s.Page(r, "Kurulum", ""))
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.CountUsers(r.Context())
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if n > 0 {
		http.Error(w, "Kurulum zaten tamamlandı.", http.StatusForbidden)
		return
	}
	p := s.Page(r, "Kurulum", "")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form okunamadı.", http.StatusBadRequest)
		return
	}
	p.Form = r.PostForm
	company := strings.TrimSpace(r.PostFormValue("company"))
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	validateNewPassword(p, password, r.PostFormValue("password2"))
	if username == "" {
		p.Errors["username"] = "Kullanıcı adı girin."
	}
	if len(p.Errors) > 0 {
		s.HTML(w, r, http.StatusUnprocessableEntity, "setup", p)
		return
	}
	hash, err := HashPassword(password)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	id, err := s.Store.CreateUser(r.Context(), username, hash, true)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if company != "" {
		_ = s.Store.SetSetting(r.Context(), "company_name", company)
	}
	if err := s.startSession(w, r, id); err != nil {
		s.ServerError(w, r, err)
		return
	}
	SetFlash(w, "ok", "Kurulum tamamlandı. İlk ürününüzü ekleyerek başlayın.")
	http.Redirect(w, r, "/products/new", http.StatusSeeOther)
}

func validateNewPassword(p *Page, pw, confirm string) {
	switch {
	case len([]rune(pw)) < MinPasswordLen:
		p.Errors["password"] = "Parola en az 10 karakter olmalı."
	case pw != confirm:
		p.Errors["password2"] = "Parolalar eşleşmiyor."
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// loginLimiter is a fixed-window failure counter per IP.
type loginLimiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string]*window
}

type window struct {
	count int
	start time.Time
}

func newLoginLimiter(max int, w time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: w, hits: map[string]*window{}}
}

func (l *loginLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	h, ok := l.hits[ip]
	if !ok || time.Since(h.start) > l.window {
		return true
	}
	return h.count < l.max
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, v := range l.hits {
		if now.Sub(v.start) > l.window {
			delete(l.hits, k)
		}
	}
	h, ok := l.hits[ip]
	if !ok {
		h = &window{start: now}
		l.hits[ip] = h
	}
	h.count++
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	delete(l.hits, ip)
	l.mu.Unlock()
}

// Flash is a one-time message carried across a redirect.
type Flash struct {
	Kind string // "ok", "error", "info"
	Text string
}

const flashCookie = "rampa_flash"

// SetFlash stores a message for the next page view.
func SetFlash(w http.ResponseWriter, kind, text string) {
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookie,
		Value:    base64.RawURLEncoding.EncodeToString([]byte(kind + "|" + text)),
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func popFlash(w http.ResponseWriter, r *http.Request) *Flash {
	c, err := r.Cookie(flashCookie)
	if err != nil {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/", MaxAge: -1})
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	kind, text, ok := strings.Cut(string(raw), "|")
	if !ok {
		return nil
	}
	return &Flash{Kind: kind, Text: text}
}
