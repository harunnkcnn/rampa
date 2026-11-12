package app

import (
	"errors"
	"html/template"
	"net/http"
	"strconv"

	"github.com/harunnkcnn/rampa/store"
)

type receiptsData struct {
	Receipts []store.Receipt
	OpenOnly bool
}

func (s *Server) handleReceipts(w http.ResponseWriter, r *http.Request) {
	sc, err := s.ScopeOf(r)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	d := receiptsData{OpenOnly: r.URL.Query().Get("tum") != "1"}
	if d.Receipts, err = s.Store.ListReceipts(r.Context(), sc.ID(), d.OpenOnly, 200); err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, "Mal kabul", "/receipts")
	p.Data = d
	s.HTML(w, r, http.StatusOK, "receipts", p)
}

func (s *Server) handleReceiptCreate(w http.ResponseWriter, r *http.Request) {
	sc, err := s.ScopeOf(r)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	wh, _ := strconv.ParseInt(r.PostFormValue("warehouse_id"), 10, 64)
	if wh == 0 && sc.Current != nil {
		wh = sc.Current.ID
	}
	if !sc.Allows(wh) {
		SetFlash(w, "error", "Malın kabul edileceği depoyu seçin.")
		http.Redirect(w, r, "/receipts", http.StatusSeeOther)
		return
	}
	id, err := s.Store.CreateReceipt(r.Context(), wh, r.PostFormValue("supplier"), r.PostFormValue("doc_no"), UserID(r))
	if errors.Is(err, store.ErrMissingRequired) {
		SetFlash(w, "error", "Tedarikçi adını girin.")
		http.Redirect(w, r, "/receipts", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	SetFlash(w, "ok", "Kabul açıldı. Ürünleri el terminalinden okutun ya da ürün sayfasından giriş yapın.")
	http.Redirect(w, r, "/receipts/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

type receiptData struct {
	Receipt store.Receipt
	Lines   []store.ReceiptLine
}

// loadReceipt returns ErrNotFound for receipts of warehouses the user may
// not use.
func (s *Server) loadReceipt(r *http.Request) (store.Receipt, error) {
	id, ok := PathID(r, "id")
	if !ok {
		return store.Receipt{}, store.ErrNotFound
	}
	rc, err := s.Store.GetReceipt(r.Context(), id)
	if err != nil {
		return rc, err
	}
	sc, err := s.ScopeOf(r)
	if err != nil {
		return rc, err
	}
	if !sc.CanAll && !sc.Allows(rc.WarehouseID) {
		return rc, store.ErrNotFound
	}
	return rc, nil
}

func (s *Server) handleReceiptShow(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadReceipt(r)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	lines, err := s.Store.ReceiptLines(r.Context(), rc.ID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, rc.Code, "/receipts")
	p.Data = receiptData{Receipt: rc, Lines: lines}
	s.HTML(w, r, http.StatusOK, "receipt", p)
}

func (s *Server) handleReceiptClose(w http.ResponseWriter, r *http.Request) {
	rc, err := s.loadReceipt(r)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if err := s.Store.CloseReceipt(r.Context(), rc.ID); err != nil {
		SetFlash(w, "error", "Bu kabul zaten kapalı.")
	} else {
		SetFlash(w, "ok", rc.Code+" kapatıldı.")
	}
	http.Redirect(w, r, "/receipts/"+strconv.FormatInt(rc.ID, 10), http.StatusSeeOther)
}

// --- Labels ---

type labelData struct {
	Company string
	Labels  []label
	Back    string
}

type label struct {
	Code    string
	Barcode template.HTML
	Title   string
	Lines   []string
	Big     string
}

func (s *Server) renderLabels(w http.ResponseWriter, r *http.Request, d labelData) {
	d.Company, _ = s.Store.Setting(r.Context(), "company_name", "")
	s.Partial(w, r, http.StatusOK, "labels", "labels-page", d)
}

func (s *Server) handlePackageLabel(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	pk, err := s.Store.GetPackage(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	sh, err := s.Store.GetShipment(r.Context(), pk.ShipmentID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if sc, err := s.ScopeOf(r); err != nil || (!sc.CanAll && !sc.Allows(sh.WarehouseID)) {
		s.NotFound(w, r)
		return
	}
	all, err := s.Store.ListPackages(r.Context(), pk.ShipmentID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	lb := label{
		Code:    pk.Code(),
		Barcode: Code128SVG(pk.Code(), 60),
		Title:   sh.Customer,
		Big:     pk.KindLabel() + " " + strconv.Itoa(pk.Seq) + " / " + strconv.Itoa(len(all)),
	}
	if sh.Plate != "" {
		lb.Lines = append(lb.Lines, "Plaka "+sh.Plate)
	}
	lb.Lines = append(lb.Lines, sh.Code+" · "+strconv.Itoa(len(pk.Items))+" çeşit, "+pk.Units().String()+" birim")
	back := r.URL.Query().Get("back")
	s.renderLabels(w, r, labelData{Labels: []label{lb}, Back: safeNext(back)})
}

func (s *Server) handleBinLabels(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	wh, err := s.Store.GetWarehouse(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	bins, err := s.Store.ListBins(r.Context(), id)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	d := labelData{Back: "/settings/warehouses?depo=" + strconv.FormatInt(id, 10)}
	for _, b := range bins {
		d.Labels = append(d.Labels, label{Code: b.Code, Barcode: Code128SVG(b.Code, 60), Title: wh.Label(), Big: b.Code})
	}
	s.renderLabels(w, r, d)
}
