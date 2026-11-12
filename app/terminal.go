package app

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/harunnkcnn/rampa/store"
)

// The handheld screens (/t) are built for 480 px wide terminals with a
// barcode reader that types the code and presses Enter. They are plain
// server-rendered forms: every action works without JavaScript, which only
// adds Enter-to-next-field, F-key shortcuts and an error beep. The markup
// and CSS avoid features older embedded browsers lack.

// TKey is a footer shortcut bound to a hardware key.
type TKey struct {
	Key   string // F1…F4
	Label string
	Href  string // link target, or
	Form  string // id of a form to submit
}

// TPage is the data passed to every terminal template.
type TPage struct {
	Title     string
	User      *store.User
	CSRF      string
	Warehouse *store.Warehouse
	Flash     *Flash
	Back      string
	Keys      []TKey
	Data      any
	Version   string
	Company   string
	Form      url.Values
}

// Val returns a form or query value.
func (p *TPage) Val(k string) string { return p.Form.Get(k) }

func (s *Server) tpage(w http.ResponseWriter, r *http.Request, title, back string) *TPage {
	p := &TPage{Title: title, Back: back, Version: Version, Form: r.URL.Query(), Flash: popFlash(w, r)}
	if ss := SessionFrom(r.Context()); ss != nil {
		u := ss.User
		p.User, p.CSRF = &u, ss.CSRF
		if sc, err := s.ScopeOf(r); err == nil {
			p.Warehouse = sc.Current
		}
	}
	p.Company, _ = s.Store.Setting(r.Context(), "company_name", "")
	if back != "" {
		p.Keys = append(p.Keys, TKey{Key: "F1", Label: "Geri", Href: back})
	}
	return p
}

func (s *Server) thtml(w http.ResponseWriter, r *http.Request, status int, page string, p *TPage) {
	w.Header().Set("Cache-Control", "no-store")
	if err := s.term.exec(w, status, page, "layout", p); err != nil {
		s.ServerError(w, r, err)
	}
}

// tback sets a one-time message and returns to target.
func tback(w http.ResponseWriter, r *http.Request, kind, text, target string) {
	SetFlash(w, kind, text)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// T registers a terminal route: a session is required, and every screen
// except the warehouse picker needs a chosen warehouse.
func (s *Server) T(pattern string, h http.HandlerFunc) {
	s.mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) == nil {
			http.Redirect(w, r, "/t/giris", http.StatusSeeOther)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/t/depo") {
			sc, err := s.ScopeOf(r)
			if err != nil {
				s.ServerError(w, r, err)
				return
			}
			if sc.Current == nil {
				http.Redirect(w, r, "/t/depo", http.StatusSeeOther)
				return
			}
		}
		h(w, r)
	}))
}

func (s *Server) terminalRoutes() {
	s.mux.HandleFunc("GET /t/giris", s.tLoginForm)
	s.mux.HandleFunc("POST /t/giris", s.tLogin)
	s.mux.HandleFunc("POST /t/cikis", s.tLogout)

	s.T("GET /t/depo", s.tDepoForm)
	s.T("POST /t/depo", s.tDepo)
	s.T("GET /t", s.tMenu)
	s.T("GET /t/{$}", s.tMenu)

	s.T("GET /t/kabul", s.tReceipts)
	s.T("POST /t/kabul", s.tReceiptCreate)
	s.T("GET /t/kabul/{id}", s.tReceipt)
	s.T("POST /t/kabul/{id}", s.tReceive)
	s.T("POST /t/kabul/{id}/geri", s.tReceiveUndo)
	s.T("POST /t/kabul/{id}/bitir", s.tReceiptClose)

	s.T("GET /t/sevk", s.tShipments)
	s.T("GET /t/sevk/{id}", s.tPick)
	s.T("POST /t/sevk/{id}", s.tPickScan)
	s.T("POST /t/sevk/{id}/yukle", s.tPickLoad)

	s.T("GET /t/sayim", s.tCountStart)
	s.T("POST /t/sayim", s.tCountOpen)
	s.T("GET /t/sayim/{id}", s.tCount)
	s.T("POST /t/sayim/{id}", s.tCountScan)
	s.T("GET /t/sayim/{id}/bitir", s.tCountReview)
	s.T("POST /t/sayim/{id}/bitir", s.tCountFinish)
	s.T("POST /t/sayim/{id}/iptal", s.tCountCancel)

	s.T("GET /t/hucre", s.tLookup)
	s.T("GET /t/transfer", s.tTransferForm)
	s.T("POST /t/transfer", s.tTransfer)

	s.T("GET /t/paket", s.tPackShipments)
	s.T("GET /t/paket/{id}", s.tPack)
	s.T("POST /t/paket/{id}", s.tPackScan)
	s.T("POST /t/paket/{id}/yeni", s.tPackNew)
	s.T("POST /t/paket/{id}/kapat", s.tPackClose)
}

// --- Sign-in and warehouse ---

func (s *Server) tLoginForm(w http.ResponseWriter, r *http.Request) {
	if SessionFrom(r.Context()) != nil {
		http.Redirect(w, r, "/t", http.StatusSeeOther)
		return
	}
	s.thtml(w, r, http.StatusOK, "giris", s.tpage(w, r, "Giriş", ""))
}

func (s *Server) tLogin(w http.ResponseWriter, r *http.Request) {
	p := s.tpage(w, r, "Giriş", "")
	p.Form = r.PostForm
	username := strings.TrimSpace(r.PostFormValue("kullanici"))
	pin := strings.TrimSpace(r.PostFormValue("pin"))
	ip, userKey := clientIP(r), "u:"+strings.ToLower(username)
	if !s.limiter.allow(ip) || !s.limiter.allow(userKey) {
		p.Flash = &Flash{Kind: "error", Text: "Çok fazla hatalı deneme. 15 dakika sonra tekrar deneyin."}
		s.thtml(w, r, http.StatusTooManyRequests, "giris", p)
		return
	}
	u, err := s.Store.GetUserByUsername(r.Context(), username)
	hash := u.PINHash
	if errors.Is(err, store.ErrNotFound) || hash == "" {
		hash = dummyHash()
	} else if err != nil {
		s.ServerError(w, r, err)
		return
	}
	ok, _ := CheckPassword(hash, pin)
	if !ok || u.ID == 0 || u.PINHash == "" {
		s.limiter.fail(ip)
		s.limiter.fail(userKey)
		p.Flash = &Flash{Kind: "error", Text: "Kullanıcı ya da PIN hatalı."}
		s.thtml(w, r, http.StatusUnauthorized, "giris", p)
		return
	}
	s.limiter.reset(ip)
	s.limiter.reset(userKey)
	if err := s.openSession(w, r, u.ID, true); err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, "/t/depo", http.StatusSeeOther)
}

func (s *Server) tLogout(w http.ResponseWriter, r *http.Request) {
	if ss := SessionFrom(r.Context()); ss != nil {
		_ = s.Store.DeleteSession(r.Context(), ss.TokenHash)
	}
	s.clearSessionCookie(w)
	http.Redirect(w, r, "/t/giris", http.StatusSeeOther)
}

func (s *Server) tDepoForm(w http.ResponseWriter, r *http.Request) {
	sc, err := s.ScopeOf(r)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	ss := SessionFrom(r.Context())
	// One warehouse: nothing to choose.
	if len(sc.Warehouses) == 1 {
		if err := s.Store.SetSessionWarehouse(r.Context(), ss.TokenHash, sc.Warehouses[0].ID); err != nil {
			s.ServerError(w, r, err)
			return
		}
		http.Redirect(w, r, "/t", http.StatusSeeOther)
		return
	}
	back := ""
	if sc.Current != nil {
		back = "/t"
	}
	p := s.tpage(w, r, "Depo seçimi", back)
	p.Data = sc.Warehouses
	s.thtml(w, r, http.StatusOK, "depo", p)
}

func (s *Server) tDepo(w http.ResponseWriter, r *http.Request) {
	sc, err := s.ScopeOf(r)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	id, _ := strconv.ParseInt(r.PostFormValue("depo"), 10, 64)
	if !sc.Allows(id) {
		tback(w, r, "error", "Bu depoya yetkiniz yok.", "/t/depo")
		return
	}
	if err := s.Store.SetSessionWarehouse(r.Context(), SessionFrom(r.Context()).TokenHash, id); err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, "/t", http.StatusSeeOther)
}

type menuData struct {
	OpenReceipts int
	Drafts       int
	ToPack       int
	MultiDepot   bool
}

func (s *Server) tMenu(w http.ResponseWriter, r *http.Request) {
	sc, _ := s.ScopeOf(r)
	wh := sc.Current.ID
	var d menuData
	d.MultiDepot = len(sc.Warehouses) > 1
	if rs, err := s.Store.ListReceipts(r.Context(), wh, true, 100); err == nil {
		d.OpenReceipts = len(rs)
	}
	if sh, err := s.Store.ListShipments(r.Context(), store.ShipmentFilter{WarehouseID: wh, Status: store.ShipDraft}); err == nil {
		d.Drafts = len(sh)
	}
	if sh, err := s.Store.ListShipments(r.Context(), store.ShipmentFilter{WarehouseID: wh, Open: true}); err == nil {
		d.ToPack = len(sh)
	}
	p := s.tpage(w, r, "Menü", "")
	p.Data = d
	p.Keys = []TKey{{Key: "F4", Label: "Çıkış", Form: "logout"}}
	if d.MultiDepot {
		p.Keys = append([]TKey{{Key: "F3", Label: "Depo", Href: "/t/depo"}}, p.Keys...)
	}
	s.thtml(w, r, http.StatusOK, "menu", p)
}

// --- Shared scan helpers ---

// scanInput is the usual bin / quantity / code triple of a scan screen.
type scanInput struct {
	Bin  store.Bin
	Qty  store.Qty
	Code string
}

// readScan resolves the bin (GENEL when empty and allowDefault), the
// quantity (1 when empty) and the code. A non-empty error message is meant
// for the user.
func (s *Server) readScan(r *http.Request, wh int64, allowDefault bool) (scanInput, string, error) {
	var in scanInput
	binCode := strings.TrimSpace(r.PostFormValue("hucre"))
	in.Code = strings.TrimSpace(r.PostFormValue("kod"))
	var err error
	switch {
	case binCode == "" && allowDefault:
		in.Bin, err = s.Store.DefaultBin(r.Context(), wh)
	case binCode == "":
		return in, "Önce hücre etiketini okutun.", nil
	default:
		in.Bin, err = s.Store.FindBin(r.Context(), wh, binCode)
	}
	if errors.Is(err, store.ErrBinNotFound) {
		return in, store.NormalizeCode(binCode) + " hücresi bu depoda yok.", nil
	}
	if err != nil {
		return in, "", err
	}
	in.Qty = store.QtyFromInt(1)
	if q := strings.TrimSpace(r.PostFormValue("miktar")); q != "" {
		v, err := store.ParseQty(q)
		if err != nil || v == 0 {
			return in, "Miktar sayı olmalı.", nil
		}
		in.Qty = v
	}
	return in, "", nil
}

func withBin(base, bin string) string {
	if bin == "" {
		return base
	}
	return base + "?hucre=" + url.QueryEscape(bin)
}

// scanError turns store errors into a terminal message, or "" if unexpected.
func scanError(err error) string {
	if errors.Is(err, store.ErrUnknownCode) {
		return "Barkod tanınmadı"
	}
	return strings.TrimSuffix(UserMessage(err), ".")
}

// --- Mal kabul ---

func (s *Server) tWarehouse(r *http.Request) *store.Warehouse {
	sc, _ := s.ScopeOf(r)
	return sc.Current
}

func (s *Server) tReceipts(w http.ResponseWriter, r *http.Request) {
	wh := s.tWarehouse(r)
	list, err := s.Store.ListReceipts(r.Context(), wh.ID, true, 30)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.tpage(w, r, "Mal kabul", "/t")
	p.Data = list
	s.thtml(w, r, http.StatusOK, "kabul_liste", p)
}

func (s *Server) tReceiptCreate(w http.ResponseWriter, r *http.Request) {
	wh := s.tWarehouse(r)
	id, err := s.Store.CreateReceipt(r.Context(), wh.ID, r.PostFormValue("tedarikci"), r.PostFormValue("irsaliye"), UserID(r))
	if errors.Is(err, store.ErrMissingRequired) {
		tback(w, r, "error", "Tedarikçi adını girin.", "/t/kabul")
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, "/t/kabul/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

// tReceiptFor loads a receipt of the current warehouse.
func (s *Server) tReceiptFor(w http.ResponseWriter, r *http.Request) (store.Receipt, bool) {
	id, ok := PathID(r, "id")
	rc, err := s.Store.GetReceipt(r.Context(), id)
	if !ok || errors.Is(err, store.ErrNotFound) || (err == nil && rc.WarehouseID != s.tWarehouse(r).ID) {
		tback(w, r, "error", "Kabul bulunamadı.", "/t/kabul")
		return rc, false
	}
	if err != nil {
		s.ServerError(w, r, err)
		return rc, false
	}
	return rc, true
}

type receiveData struct {
	Receipt store.Receipt
	Lines   []store.ReceiptLine
}

func (s *Server) tReceipt(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.tReceiptFor(w, r)
	if !ok {
		return
	}
	lines, err := s.Store.ReceiptLines(r.Context(), rc.ID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if len(lines) > 8 {
		lines = lines[:8]
	}
	p := s.tpage(w, r, "Kabul "+rc.Code[len(rc.Code)-4:], "/t/kabul")
	p.Data = receiveData{Receipt: rc, Lines: lines}
	if rc.Open() {
		p.Keys = append(p.Keys, TKey{Key: "F2", Label: "Geri al", Form: "undo"}, TKey{Key: "F4", Label: "Bitir", Form: "finish"})
	}
	s.thtml(w, r, http.StatusOK, "kabul", p)
}

func (s *Server) tReceive(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.tReceiptFor(w, r)
	if !ok {
		return
	}
	self := "/t/kabul/" + strconv.FormatInt(rc.ID, 10)
	in, msg, err := s.readScan(r, rc.WarehouseID, true)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if msg != "" {
		tback(w, r, "error", msg, self)
		return
	}
	if in.Code == "" { // only the bin was scanned
		http.Redirect(w, r, withBin(self, in.Bin.Code), http.StatusSeeOther)
		return
	}
	if in.Qty < 0 {
		tback(w, r, "error", "Kabulde miktar eksi olamaz; yanlış okutmayı F2 ile geri alın.", withBin(self, in.Bin.Code))
		return
	}
	prod, err := s.Store.FindProductByCode(r.Context(), in.Code)
	if err == nil {
		err = s.Store.Receive(r.Context(), rc.ID, in.Bin.ID, prod.ID, in.Qty, UserID(r))
	}
	if err != nil {
		if m := scanError(err); m != "" {
			tback(w, r, "error", m+": "+in.Code, withBin(self, in.Bin.Code))
			return
		}
		s.ServerError(w, r, err)
		return
	}
	tback(w, r, "ok", "+"+in.Qty.String()+" "+prod.Unit+" "+prod.Name+" → "+in.Bin.Code, withBin(self, in.Bin.Code))
}

func (s *Server) tReceiveUndo(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.tReceiptFor(w, r)
	if !ok {
		return
	}
	self := withBin("/t/kabul/"+strconv.FormatInt(rc.ID, 10), r.PostFormValue("hucre"))
	switch err := s.Store.UndoLastReceiptLine(r.Context(), rc.ID, UserID(r)); {
	case errors.Is(err, store.ErrNoChange):
		tback(w, r, "error", "Geri alınacak okutma yok.", self)
	case errors.Is(err, store.ErrInvalidState):
		tback(w, r, "error", "Son okutulan mal hücreden çıkmış; geri alınamaz.", self)
	case err != nil:
		s.ServerError(w, r, err)
	default:
		tback(w, r, "ok", "Son okutma geri alındı.", self)
	}
}

func (s *Server) tReceiptClose(w http.ResponseWriter, r *http.Request) {
	rc, ok := s.tReceiptFor(w, r)
	if !ok {
		return
	}
	if err := s.Store.CloseReceipt(r.Context(), rc.ID); err != nil {
		tback(w, r, "error", "Kabul zaten kapalı.", "/t/kabul")
		return
	}
	tback(w, r, "ok", rc.Code+" kapandı: "+strconv.Itoa(rc.Lines)+" çeşit, "+rc.Units.String()+" birim.", "/t/kabul")
}

// --- Sevkiyat toplama ---

func (s *Server) tShipments(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListShipments(r.Context(), store.ShipmentFilter{WarehouseID: s.tWarehouse(r).ID, Status: store.ShipDraft})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.tpage(w, r, "Sevkiyat", "/t")
	p.Data = list
	s.thtml(w, r, http.StatusOK, "sevk_liste", p)
}

func (s *Server) tShipmentFor(w http.ResponseWriter, r *http.Request, back string) (store.Shipment, bool) {
	id, ok := PathID(r, "id")
	sh, err := s.Store.GetShipment(r.Context(), id)
	if !ok || errors.Is(err, store.ErrNotFound) || (err == nil && sh.WarehouseID != s.tWarehouse(r).ID) {
		tback(w, r, "error", "Sevkiyat bu depoda bulunamadı.", back)
		return sh, false
	}
	if err != nil {
		s.ServerError(w, r, err)
		return sh, false
	}
	return sh, true
}

type pickLine struct {
	Item  store.ShipmentItem
	Where []store.BinStock
}

type pickData struct {
	Shipment store.Shipment
	Open     []pickLine
	Done     []store.ShipmentItem
}

func (s *Server) tPick(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/sevk")
	if !ok {
		return
	}
	d := pickData{Shipment: sh}
	for _, it := range sh.Items {
		if it.Picked >= it.Qty {
			d.Done = append(d.Done, it)
			continue
		}
		where, err := s.Store.ProductBins(r.Context(), it.ProductID, sh.WarehouseID)
		if err != nil {
			s.ServerError(w, r, err)
			return
		}
		if len(where) > 3 {
			where = where[:3]
		}
		d.Open = append(d.Open, pickLine{Item: it, Where: where})
	}
	p := s.tpage(w, r, sh.Code, "/t/sevk")
	p.Data = d
	if sh.Editable() {
		p.Keys = append(p.Keys, TKey{Key: "F4", Label: "Yükle", Form: "load"})
	}
	s.thtml(w, r, http.StatusOK, "sevk", p)
}

func (s *Server) tPickScan(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/sevk")
	if !ok {
		return
	}
	self := "/t/sevk/" + strconv.FormatInt(sh.ID, 10)
	in, msg, err := s.readScan(r, sh.WarehouseID, false)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if msg != "" {
		tback(w, r, "error", msg, self)
		return
	}
	if in.Code == "" {
		http.Redirect(w, r, withBin(self, in.Bin.Code), http.StatusSeeOther)
		return
	}
	if in.Qty < 0 {
		tback(w, r, "error", "Toplamada miktar eksi olamaz.", withBin(self, in.Bin.Code))
		return
	}
	prod, err := s.Store.FindProductByCode(r.Context(), in.Code)
	var res store.PickResult
	if err == nil {
		res, err = s.Store.AddPick(r.Context(), sh.ID, in.Bin.ID, prod.ID, in.Qty, UserID(r))
	}
	if err != nil {
		if m := scanError(err); m != "" {
			tback(w, r, "error", m+": "+in.Code, withBin(self, in.Bin.Code))
			return
		}
		s.ServerError(w, r, err)
		return
	}
	text := res.Item.Name + " " + res.Item.Picked.String() + "/" + res.Item.Qty.String()
	if res.Done {
		text = "Tümü toplandı. F4 ile yükleyin."
	}
	tback(w, r, "ok", text, withBin(self, in.Bin.Code))
}

func (s *Server) tPickLoad(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/sevk")
	if !ok {
		return
	}
	if err := s.Store.LoadShipment(r.Context(), sh.ID, UserID(r)); err != nil {
		if m := scanError(err); m != "" {
			tback(w, r, "error", m, "/t/sevk/"+strconv.FormatInt(sh.ID, 10))
			return
		}
		s.ServerError(w, r, err)
		return
	}
	tback(w, r, "ok", sh.Code+" yüklendi, stoktan düşüldü.", "/t/sevk")
}

// --- Sayım ---

func (s *Server) tCountStart(w http.ResponseWriter, r *http.Request) {
	p := s.tpage(w, r, "Sayım", "/t")
	s.thtml(w, r, http.StatusOK, "sayim_basla", p)
}

func (s *Server) tCountOpen(w http.ResponseWriter, r *http.Request) {
	wh := s.tWarehouse(r)
	bin, err := s.Store.FindBin(r.Context(), wh.ID, r.PostFormValue("hucre"))
	if errors.Is(err, store.ErrBinNotFound) {
		tback(w, r, "error", "Hücre bulunamadı: "+store.NormalizeCode(r.PostFormValue("hucre")), "/t/sayim")
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	id, err := s.Store.StartCount(r.Context(), bin.ID, UserID(r))
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, "/t/sayim/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) tCountFor(w http.ResponseWriter, r *http.Request) (store.Count, bool) {
	id, ok := PathID(r, "id")
	c, err := s.Store.GetCount(r.Context(), id)
	if !ok || errors.Is(err, store.ErrNotFound) || (err == nil && c.WarehouseID != s.tWarehouse(r).ID) {
		tback(w, r, "error", "Sayım bulunamadı.", "/t/sayim")
		return c, false
	}
	if err != nil {
		s.ServerError(w, r, err)
		return c, false
	}
	if c.Status != "open" {
		tback(w, r, "info", "Bu sayım kapanmış.", "/t/sayim")
		return c, false
	}
	return c, true
}

func (s *Server) tCount(w http.ResponseWriter, r *http.Request) {
	c, ok := s.tCountFor(w, r)
	if !ok {
		return
	}
	p := s.tpage(w, r, "Sayım "+c.BinCode, "/t/sayim")
	p.Data = c
	p.Keys = append(p.Keys, TKey{Key: "F3", Label: "İptal", Form: "cancel"}, TKey{Key: "F4", Label: "Bitir", Href: "/t/sayim/" + strconv.FormatInt(c.ID, 10) + "/bitir"})
	s.thtml(w, r, http.StatusOK, "sayim", p)
}

func (s *Server) tCountScan(w http.ResponseWriter, r *http.Request) {
	c, ok := s.tCountFor(w, r)
	if !ok {
		return
	}
	self := "/t/sayim/" + strconv.FormatInt(c.ID, 10)
	code := strings.TrimSpace(r.PostFormValue("kod"))
	qty := store.QtyFromInt(1)
	if q := strings.TrimSpace(r.PostFormValue("miktar")); q != "" {
		v, err := store.ParseQty(q)
		if err != nil || v == 0 {
			tback(w, r, "error", "Miktar sayı olmalı; düzeltme için eksi girin, örn. -1.", self)
			return
		}
		qty = v
	}
	if code == "" {
		http.Redirect(w, r, self, http.StatusSeeOther)
		return
	}
	prod, err := s.Store.FindProductByCode(r.Context(), code)
	var total store.Qty
	if err == nil {
		total, err = s.Store.AddCount(r.Context(), c.ID, prod.ID, qty)
	}
	switch {
	case errors.Is(err, store.ErrInvalidQty):
		tback(w, r, "error", "Sayılan miktar sıfırın altına inemez.", self)
	case err != nil && scanError(err) != "":
		tback(w, r, "error", scanError(err)+": "+code, self)
	case err != nil:
		s.ServerError(w, r, err)
	default:
		tback(w, r, "ok", prod.Name+": "+total.String()+" "+prod.Unit, self)
	}
}

func (s *Server) tCountReview(w http.ResponseWriter, r *http.Request) {
	c, ok := s.tCountFor(w, r)
	if !ok {
		return
	}
	p := s.tpage(w, r, "Sayımı bitir", "/t/sayim/"+strconv.FormatInt(c.ID, 10))
	p.Data = c
	p.Keys = append(p.Keys, TKey{Key: "F4", Label: "Onayla", Form: "confirm"})
	s.thtml(w, r, http.StatusOK, "sayim_bitir", p)
}

func (s *Server) tCountFinish(w http.ResponseWriter, r *http.Request) {
	c, ok := s.tCountFor(w, r)
	if !ok {
		return
	}
	n, err := s.Store.FinishCount(r.Context(), c.ID, UserID(r))
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	text := c.BinCode + " sayımı kaydedildi; fark yok."
	if n > 0 {
		text = c.BinCode + " sayımı kaydedildi; " + strconv.Itoa(n) + " üründe stok düzeltildi."
	}
	tback(w, r, "ok", text, "/t/sayim")
}

func (s *Server) tCountCancel(w http.ResponseWriter, r *http.Request) {
	c, ok := s.tCountFor(w, r)
	if !ok {
		return
	}
	if err := s.Store.CancelCount(r.Context(), c.ID); err != nil {
		s.ServerError(w, r, err)
		return
	}
	tback(w, r, "info", c.BinCode+" sayımı iptal edildi; stok değişmedi.", "/t/sayim")
}

// --- Hücre takibi ---

type lookupData struct {
	Code    string
	Bin     *store.Bin
	Product *store.Product
	Rows    []store.BinStock
	Message string
}

func (s *Server) tLookup(w http.ResponseWriter, r *http.Request) {
	wh := s.tWarehouse(r)
	d := lookupData{Code: strings.TrimSpace(r.URL.Query().Get("kod"))}
	if d.Code != "" {
		if b, err := s.Store.FindBin(r.Context(), wh.ID, d.Code); err == nil {
			d.Bin = &b
			if d.Rows, err = s.Store.BinContents(r.Context(), b.ID); err != nil {
				s.ServerError(w, r, err)
				return
			}
		} else if p, err := s.Store.FindProductByCode(r.Context(), d.Code); err == nil {
			d.Product = &p
			if d.Rows, err = s.Store.ProductBins(r.Context(), p.ID, wh.ID); err != nil {
				s.ServerError(w, r, err)
				return
			}
		} else {
			d.Message = "Bu kod ne hücre ne ürün: " + d.Code
		}
	}
	p := s.tpage(w, r, "Hücre takibi", "/t")
	p.Data = d
	p.Keys = append(p.Keys, TKey{Key: "F2", Label: "Transfer", Href: "/t/transfer"})
	s.thtml(w, r, http.StatusOK, "hucre", p)
}

func (s *Server) tTransferForm(w http.ResponseWriter, r *http.Request) {
	p := s.tpage(w, r, "Transfer", "/t/hucre")
	s.thtml(w, r, http.StatusOK, "transfer", p)
}

func (s *Server) tTransfer(w http.ResponseWriter, r *http.Request) {
	wh := s.tWarehouse(r)
	src := store.NormalizeCode(r.PostFormValue("kaynak"))
	dst := store.NormalizeCode(r.PostFormValue("hedef"))
	code := strings.TrimSpace(r.PostFormValue("kod"))
	keep := "/t/transfer?kaynak=" + url.QueryEscape(src) + "&hedef=" + url.QueryEscape(dst) + "&kod=" + url.QueryEscape(code)
	if src == "" || dst == "" || code == "" {
		tback(w, r, "error", "Kaynak hücre, ürün ve hedef hücre gerekli.", keep)
		return
	}
	qty := store.QtyFromInt(1)
	if q := strings.TrimSpace(r.PostFormValue("miktar")); q != "" {
		v, err := store.ParseQty(q)
		if err != nil || v <= 0 {
			tback(w, r, "error", "Miktar sıfırdan büyük olmalı.", keep)
			return
		}
		qty = v
	}
	from, err := s.Store.FindBin(r.Context(), wh.ID, src)
	if err != nil {
		tback(w, r, "error", src+" hücresi bu depoda yok.", keep)
		return
	}
	to, err := s.Store.FindBin(r.Context(), wh.ID, dst)
	if err != nil {
		tback(w, r, "error", dst+" hücresi bu depoda yok.", keep)
		return
	}
	prod, err := s.Store.FindProductByCode(r.Context(), code)
	if err == nil {
		err = s.Store.Transfer(r.Context(), prod.ID, from.ID, to.ID, qty, UserID(r))
	}
	if err != nil {
		if m := scanError(err); m != "" {
			tback(w, r, "error", m, keep)
			return
		}
		s.ServerError(w, r, err)
		return
	}
	tback(w, r, "ok", qty.String()+" "+prod.Unit+" "+prod.Name+": "+from.Code+" → "+to.Code,
		"/t/transfer?kaynak="+url.QueryEscape(from.Code))
}

// --- Paketleme ---

func (s *Server) tPackShipments(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListShipments(r.Context(), store.ShipmentFilter{WarehouseID: s.tWarehouse(r).ID, Open: true})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.tpage(w, r, "Paketleme", "/t")
	p.Data = list
	s.thtml(w, r, http.StatusOK, "paket_liste", p)
}

type packData struct {
	Shipment store.Shipment
	Packages []store.Package
	Current  *store.Package
	Left     []store.ShipmentItem
}

func (s *Server) tPack(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/paket")
	if !ok {
		return
	}
	pkgs, err := s.Store.ListPackages(r.Context(), sh.ID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	d := packData{Shipment: sh, Packages: pkgs}
	for i := range pkgs {
		if pkgs[i].Open() {
			d.Current = &pkgs[i]
		}
	}
	for _, it := range sh.Items {
		if it.Packed < it.Qty {
			d.Left = append(d.Left, it)
		}
	}
	p := s.tpage(w, r, "Paket "+sh.Code[len(sh.Code)-4:], "/t/paket")
	p.Data = d
	if d.Current != nil {
		p.Keys = append(p.Keys, TKey{Key: "F4", Label: "Kapat", Form: "close"})
	} else if len(d.Left) > 0 {
		p.Keys = append(p.Keys, TKey{Key: "F2", Label: "Koli", Form: "new-koli"}, TKey{Key: "F3", Label: "Palet", Form: "new-palet"})
	}
	s.thtml(w, r, http.StatusOK, "paket", p)
}

func (s *Server) tPackNew(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/paket")
	if !ok {
		return
	}
	self := "/t/paket/" + strconv.FormatInt(sh.ID, 10)
	kind := r.PostFormValue("tur")
	if _, err := s.Store.CreatePackage(r.Context(), sh.ID, kind); err != nil {
		if m := scanError(err); m != "" {
			tback(w, r, "error", m, self)
			return
		}
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, self, http.StatusSeeOther)
}

func (s *Server) tPackScan(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/paket")
	if !ok {
		return
	}
	self := "/t/paket/" + strconv.FormatInt(sh.ID, 10)
	pkgID, _ := strconv.ParseInt(r.PostFormValue("paket"), 10, 64)
	code := strings.TrimSpace(r.PostFormValue("kod"))
	if code == "" {
		http.Redirect(w, r, self, http.StatusSeeOther)
		return
	}
	qty := store.QtyFromInt(1)
	if q := strings.TrimSpace(r.PostFormValue("miktar")); q != "" {
		v, err := store.ParseQty(q)
		if err != nil || v <= 0 {
			tback(w, r, "error", "Miktar sıfırdan büyük olmalı.", self)
			return
		}
		qty = v
	}
	pk, err := s.Store.GetPackage(r.Context(), pkgID)
	if err != nil || pk.ShipmentID != sh.ID {
		tback(w, r, "error", "Önce koli ya da palet açın.", self)
		return
	}
	prod, err := s.Store.FindProductByCode(r.Context(), code)
	if err == nil {
		err = s.Store.PackItem(r.Context(), pk.ID, prod.ID, qty)
	}
	if err != nil {
		if m := scanError(err); m != "" {
			tback(w, r, "error", m+": "+code, self)
			return
		}
		s.ServerError(w, r, err)
		return
	}
	tback(w, r, "ok", "+"+qty.String()+" "+prod.Name+" → "+pk.Code(), self)
}

func (s *Server) tPackClose(w http.ResponseWriter, r *http.Request) {
	sh, ok := s.tShipmentFor(w, r, "/t/paket")
	if !ok {
		return
	}
	self := "/t/paket/" + strconv.FormatInt(sh.ID, 10)
	pkgID, _ := strconv.ParseInt(r.PostFormValue("paket"), 10, 64)
	pk, err := s.Store.GetPackage(r.Context(), pkgID)
	if err != nil || pk.ShipmentID != sh.ID {
		tback(w, r, "error", "Kapatılacak paket bulunamadı.", self)
		return
	}
	deleted, err := s.Store.ClosePackage(r.Context(), pk.ID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if deleted {
		tback(w, r, "info", "Boş paket silindi.", self)
		return
	}
	tback(w, r, "ok", pk.Code()+" kapandı. Etiketi panelden yazdırın.", self)
}
