package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/harunnkcnn/rampa/store"
)

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func newTestServer(t *testing.T, exts ...Extension) (*client, *Server) {
	t.Helper()
	PasswordIterations = 1000
	st, err := store.Open(filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(context.Background(), Config{}, st, log, exts...)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: ts.URL, http: &http.Client{Jar: jar}}, srv
}

var csrfRe = regexp.MustCompile(`name="_csrf" value="([^"]+)"`)

func (c *client) get(path string) (int, string) {
	c.t.Helper()
	res, err := c.http.Get(c.base + path)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if m := csrfRe.FindStringSubmatch(string(b)); m != nil {
		c.csrf = m[1]
	}
	return res.StatusCode, string(b)
}

func (c *client) post(path string, form url.Values, headers ...string) (int, string, http.Header) {
	c.t.Helper()
	if c.csrf != "" && form.Get("_csrf") == "" {
		form.Set("_csrf", c.csrf)
	}
	req, _ := http.NewRequest(http.MethodPost, c.base+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if m := csrfRe.FindStringSubmatch(string(b)); m != nil {
		c.csrf = m[1]
	}
	return res.StatusCode, string(b), res.Header
}

func mustContain(t *testing.T, body string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Fatalf("response missing %q", p)
		}
	}
}

func setupAdmin(t *testing.T, c *client) {
	t.Helper()
	code, body := c.get("/")
	if code != 200 || !strings.Contains(body, "Rampa'yı kur") {
		t.Fatalf("first run should land on setup, got %d", code)
	}
	code, _, _ = c.post("/setup", url.Values{
		"company": {"Dinamik Depo"}, "username": {"harun"},
		"password": {"cok-gizli-parola"}, "password2": {"cok-gizli-parola"},
	})
	if code != 200 {
		t.Fatalf("setup = %d", code)
	}
	c.get("/")
}

func TestEndToEnd(t *testing.T) {
	c, srv := newTestServer(t)
	setupAdmin(t, c)

	if code, _, _ := c.post("/setup", url.Values{"username": {"x"}, "password": {"0123456789"}, "password2": {"0123456789"}}); code != http.StatusForbidden {
		t.Fatalf("second setup must be refused, got %d", code)
	}

	code, body, _ := c.post("/products", url.Values{"sku": {"vida-m8"}, "name": {"M8 civata"}, "unit": {"adet"}, "min_stock": {"50"}, "weight_kg": {"0,02"}})
	if code != 200 {
		t.Fatalf("create product = %d", code)
	}
	mustContain(t, body, "M8 civata", "VIDA-M8", "Sarı çizgi kritik seviye: 50")

	code, body, _ = c.post("/products/1/movements", url.Values{"kind": {"in"}, "qty": {"120"}}, "HX-Request", "true")
	if code != 200 {
		t.Fatalf("movement = %d: %s", code, body)
	}
	mustContain(t, body, `id="stock-board"`, ">120<", "Giriş kaydedildi", `hx-swap-oob="true"`)

	code, body, _ = c.post("/products/1/movements", url.Values{"kind": {"out"}, "qty": {"500"}}, "HX-Request", "true")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("overdraw should be 422, got %d", code)
	}
	mustContain(t, body, "yeterli stok yok")

	c.post("/shipments", url.Values{"customer": {"Atlas Yapı"}, "plate": {"34 abc 12"}})
	code, body, _ = c.post("/shipments/1/items", url.Values{"product_id": {"1"}, "qty": {"100"}}, "HX-Request", "true", "HX-Target", "shipment-items")
	if code != 200 {
		t.Fatalf("add item = %d", code)
	}
	mustContain(t, body, `id="shipment-items"`, "Toplam ağırlık", "2 kg")

	_, _, h := c.post("/shipments/1/load", url.Values{}, "HX-Request", "true")
	if h.Get("HX-Redirect") != "/shipments/1" {
		t.Fatalf("load redirect = %q", h.Get("HX-Redirect"))
	}
	p, _ := srv.Store.GetProduct(context.Background(), 1)
	if p.Stock != store.QtyFromInt(20) || !p.Low() {
		t.Fatalf("stock after load = %v", p.Stock)
	}
	_, body = c.get("/")
	mustContain(t, body, "Dinamik Depo", "M8 civata", "SVK-")

	_, body = c.get("/movements.csv")
	mustContain(t, body, "\xef\xbb\xbfTarih;Stok kodu", "VIDA-M8;M8 civata;Çıkış;-100")
}

func TestCSRFAndAuth(t *testing.T) {
	c, _ := newTestServer(t)
	setupAdmin(t, c)

	code, _, _ := c.post("/products", url.Values{"sku": {"A"}, "name": {"B"}, "_csrf": {"wrong"}})
	if code != http.StatusForbidden {
		t.Fatalf("bad csrf token must be 403, got %d", code)
	}
	code, _, _ = c.post("/products", url.Values{"sku": {"A"}, "name": {"B"}}, "Sec-Fetch-Site", "cross-site")
	if code != http.StatusForbidden {
		t.Fatalf("cross-site must be 403, got %d", code)
	}

	c.post("/logout", url.Values{})
	c.csrf = ""
	res, _ := c.http.Get(c.base + "/products")
	if res.Request.URL.Path != "/login" {
		t.Fatalf("signed-out user should be sent to login, got %s", res.Request.URL)
	}
	for i := 0; i < 5; i++ {
		c.post("/login", url.Values{"username": {"harun"}, "password": {"yanlis-parola"}})
	}
	code, body, _ := c.post("/login", url.Values{"username": {"harun"}, "password": {"cok-gizli-parola"}})
	if code != http.StatusTooManyRequests {
		t.Fatalf("6th attempt must be rate limited, got %d", code)
	}
	mustContain(t, body, "15 dakika")
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/products": "/products", "//evil.com": "/", "https://evil.com": "/", "/\\evil.com": "/", "": "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPassword(t *testing.T) {
	PasswordIterations = 1000
	h, err := HashPassword("parola-123456")
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := CheckPassword(h, "parola-123456"); !ok {
		t.Fatal("correct password rejected")
	}
	if ok, _ := CheckPassword(h, "parola-123457"); ok {
		t.Fatal("wrong password accepted")
	}
}
