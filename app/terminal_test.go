package app

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/harunnkcnn/rampa/store"
)

func TestExpandBinCodes(t *testing.T) {
	got := ExpandBinCodes("A-01-1..3\nB-02-08..10, RAMPA-1\n\n")
	want := []string{"A-01-1", "A-01-2", "A-01-3", "B-02-08", "B-02-09", "B-02-10", "RAMPA-1"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v", got)
	}
}

// expectFlash follows the redirect and checks the banner text.
func expectFlash(t *testing.T, body, kind, text string) {
	t.Helper()
	if !strings.Contains(body, `data-result="`+kind+`"`) || !strings.Contains(body, text) {
		i := strings.Index(body, "t-flash")
		snippet := ""
		if i >= 0 {
			snippet = body[i:min(len(body), i+200)]
		}
		t.Fatalf("want %s banner %q, got: %s", kind, text, snippet)
	}
}

func TestTerminalFlows(t *testing.T) {
	admin, srv := newTestServer(t)
	setupAdmin(t, admin)
	ctx := context.Background()
	st := srv.Store

	// Admin: a second warehouse with bins, a product with a barcode, a
	// shift worker with a PIN who may only use that warehouse.
	admin.get("/settings/warehouses")
	admin.post("/settings/branches", url.Values{"branch_name": {"İzmir Kuzey"}})
	if code, _, _ := admin.post("/settings/warehouses", url.Values{"branch_id": {"2"}, "code": {"06"}, "name": {"Satış depo"}}); code != 200 {
		t.Fatalf("create warehouse = %d", code)
	}
	admin.post("/settings/warehouses/2/bins", url.Values{"codes": {"A-01-1..2"}})
	admin.post("/products", url.Values{"sku": {"FLT-1042"}, "name": {"Yağ filtresi"}, "barcode": {"8690000001042"}})
	admin.get("/settings")
	admin.post("/settings/users", url.Values{"new_username": {"61000"}, "new_password": {"gecici-parola-1"}})
	users, _ := st.ListUsers(ctx)
	var worker store.User
	for _, u := range users {
		if u.Username == "61000" {
			worker = u
		}
	}
	admin.get("/settings")
	admin.post("/settings/users/"+itoa(worker.ID)+"/pin", url.Values{"pin": {"4321"}})
	admin.post("/settings/users/"+itoa(worker.ID)+"/warehouses", url.Values{"warehouse": {"2"}})
	if code, body := admin.get("/settings/warehouses/2/labels"); code != 200 || !strings.Contains(body, `aria-label="Barkod A-01-1"`) {
		t.Fatalf("bin labels = %d", code)
	}

	// Worker on the handheld.
	jar, _ := cookiejar.New(nil)
	h := &client{t: t, base: admin.base, http: &http.Client{Jar: jar}}
	if code, body, _ := h.post("/t/giris", url.Values{"kullanici": {"61000"}, "pin": {"1111"}}); code != http.StatusUnauthorized || !strings.Contains(body, "PIN hatalı") {
		t.Fatalf("wrong PIN = %d", code)
	}
	code, body, _ := h.post("/t/giris", url.Values{"kullanici": {"61000"}, "pin": {"4321"}})
	if code != 200 || !strings.Contains(body, "Mal kabul") || !strings.Contains(body, `class="t-depo">06<`) {
		t.Fatalf("PIN login should land on the 06 menu (single warehouse), got %d", code)
	}
	// A PIN session must not open the admin panel.
	if res, err := h.http.Get(h.base + "/products"); err != nil || res.Request.URL.Path != "/t" {
		t.Fatalf("panel with PIN session should bounce to /t, got %v", res.Request.URL)
	}

	// Mal kabul: open, scan a bin, then 2× the product with quantity 5.
	_, body, _ = h.post("/t/kabul", url.Values{"tedarikci": {"Bosch"}, "irsaliye": {"irs-77"}})
	if !strings.Contains(body, "Ürün barkodu") {
		t.Fatal("receipt scan screen not shown")
	}
	_, body, _ = h.post("/t/kabul/1", url.Values{"hucre": {"a-01-1"}})
	if !strings.Contains(body, `value="A-01-1"`) {
		t.Fatal("scanned bin should stick")
	}
	_, body, _ = h.post("/t/kabul/1", url.Values{"hucre": {"A-01-1"}, "miktar": {"5"}, "kod": {"8690000001042"}})
	expectFlash(t, body, "ok", "5 adet Yağ filtresi → A-01-1")
	h.post("/t/kabul/1", url.Values{"hucre": {"A-01-1"}, "miktar": {"5"}, "kod": {"FLT-1042"}})
	_, body, _ = h.post("/t/kabul/1", url.Values{"hucre": {"A-01-1"}, "kod": {"999"}})
	expectFlash(t, body, "error", "Barkod tanınmadı: 999")
	_, body, _ = h.post("/t/kabul/1", url.Values{"hucre": {"Z-99"}, "kod": {"FLT-1042"}})
	expectFlash(t, body, "error", "Z-99 hücresi bu depoda yok")
	_, body, _ = h.post("/t/kabul/1/bitir", url.Values{})
	expectFlash(t, body, "ok", "KBL-")

	// Hücre takibi + transfer 3 units to A-01-2.
	_, body = h.get("/t/hucre?kod=8690000001042")
	if !strings.Contains(body, "A-01-1") || !strings.Contains(body, ">10 adet<") {
		t.Fatal("product lookup should list A-01-1 with 10")
	}
	_, body, _ = h.post("/t/transfer", url.Values{"kaynak": {"A-01-1"}, "kod": {"FLT-1042"}, "miktar": {"3"}, "hedef": {"A-01-2"}})
	expectFlash(t, body, "ok", "A-01-1 → A-01-2")

	// Shipment created in the panel, picked on the handheld.
	admin.post("/scope", url.Values{"warehouse_id": {"2"}, "back": {"/shipments"}})
	admin.get("/shipments")
	admin.post("/shipments", url.Values{"customer": {"Atlas Oto"}})
	admin.post("/shipments/1/items", url.Values{"product_id": {"1"}, "qty": {"4"}})
	_, body = h.get("/t/sevk/1")
	if !strings.Contains(body, "A-01-1: 7") || strings.Contains(body, "06/A-01-1") {
		t.Fatal("pick list should suggest bins with stock")
	}
	_, body, _ = h.post("/t/sevk/1", url.Values{"hucre": {"A-01-2"}, "miktar": {"3"}, "kod": {"FLT-1042"}})
	expectFlash(t, body, "ok", "Yağ filtresi 3/4")
	_, body, _ = h.post("/t/sevk/1", url.Values{"hucre": {"A-01-2"}, "kod": {"FLT-1042"}})
	expectFlash(t, body, "error", "A-01-2 hücresinde yeterli stok yok")
	_, body, _ = h.post("/t/sevk/1", url.Values{"hucre": {"A-01-1"}, "kod": {"FLT-1042"}})
	expectFlash(t, body, "ok", "Tümü toplandı")

	// Pack before loading: one koli with all 4.
	h.post("/t/paket/1/yeni", url.Values{"tur": {"koli"}})
	_, body, _ = h.post("/t/paket/1", url.Values{"paket": {"1"}, "miktar": {"4"}, "kod": {"FLT-1042"}})
	expectFlash(t, body, "ok", "-K01")
	_, body, _ = h.post("/t/paket/1/kapat", url.Values{"paket": {"1"}})
	expectFlash(t, body, "ok", "K01 kapandı")
	if code, body := admin.get("/packages/1/label"); code != 200 || !strings.Contains(body, "Koli 1 / 1") {
		t.Fatalf("package label = %d", code)
	}

	_, body, _ = h.post("/t/sevk/1/yukle", url.Values{})
	expectFlash(t, body, "ok", "yüklendi")
	for bin, want := range map[string]int64{"A-01-1": 6, "A-01-2": 0} {
		b, _ := st.FindBin(ctx, 2, bin)
		rows, _ := st.BinContents(ctx, b.ID)
		var got store.Qty
		for _, r := range rows {
			got += r.Qty
		}
		if got != store.QtyFromInt(want) {
			t.Errorf("%s = %v, want %d", bin, got, want)
		}
	}

	// Blind count of A-01-1: shelf really has 5.
	_, body, _ = h.post("/t/sayim", url.Values{"hucre": {"A-01-1"}})
	if strings.Contains(body, ">6 adet") {
		t.Fatal("count screen must not reveal the book quantity")
	}
	h.post("/t/sayim/1", url.Values{"miktar": {"6"}, "kod": {"FLT-1042"}})
	_, body, _ = h.post("/t/sayim/1", url.Values{"miktar": {"-1"}, "kod": {"FLT-1042"}})
	expectFlash(t, body, "ok", "Yağ filtresi: 5 adet")
	_, body = h.get("/t/sayim/1/bitir")
	if !strings.Contains(body, ">-1<") {
		t.Fatal("review should show the -1 difference")
	}
	_, body, _ = h.post("/t/sayim/1/bitir", url.Values{})
	expectFlash(t, body, "ok", "1 üründe stok düzeltildi")

	// The worker cannot reach the main warehouse's records.
	admin.post("/scope", url.Values{"warehouse_id": {"0"}, "back": {"/"}})
	admin.post("/shipments", url.Values{"customer": {"Merkez müşteri"}, "warehouse_id": {"1"}})
	_, body = h.get("/t/sevk/2")
	expectFlash(t, body, "error", "bu depoda bulunamadı")
}

func itoa(n int64) string { return store.QtyFromInt(n).Input() }
