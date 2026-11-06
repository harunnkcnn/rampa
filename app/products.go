package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/harunnkcnn/rampa/store"
)

type productsData struct {
	Products []store.Product
	Query    string
	Low      bool
	Archived bool
}

func (s *Server) handleProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := productsData{
		Query:    strings.TrimSpace(q.Get("q")),
		Low:      q.Get("low") == "1",
		Archived: q.Get("archived") == "1",
	}
	var err error
	d.Products, err = s.Store.ListProducts(r.Context(), store.ProductFilter{
		Query: d.Query, LowOnly: d.Low, IncludeArchived: d.Archived,
	})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, "Ürünler", "/products")
	p.Data = d
	if IsHTMX(r) && r.Header.Get("HX-Target") == "product-rows" {
		s.Partial(w, r, http.StatusOK, "products", "product-rows", p)
		return
	}
	s.HTML(w, r, http.StatusOK, "products", p)
}

func productForm(p store.Product) map[string][]string {
	return map[string][]string{
		"sku":       {p.SKU},
		"name":      {p.Name},
		"unit":      {p.Unit},
		"location":  {p.Location},
		"min_stock": {p.MinStock.Input()},
		"weight_kg": {floatInput(p.WeightKg)},
		"volume_m3": {floatInput(p.VolumeM3)},
	}
}

func floatInput(v float64) string {
	if v == 0 {
		return ""
	}
	return strings.Replace(strconv.FormatFloat(v, 'f', -1, 64), ".", ",", 1)
}

func parseFloatField(v string) (float64, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, true
	}
	q, err := store.ParseQty(v)
	if err != nil || q < 0 {
		return 0, false
	}
	return q.Float(), true
}

// parseProduct reads the product form, collecting field errors on page.
func parseProduct(r *http.Request, page *Page) store.Product {
	f := r.PostForm
	page.Form = f
	p := store.Product{
		SKU:      f.Get("sku"),
		Name:     f.Get("name"),
		Unit:     f.Get("unit"),
		Location: f.Get("location"),
	}
	if strings.TrimSpace(p.SKU) == "" {
		page.Errors["sku"] = "Stok kodu girin."
	}
	if strings.TrimSpace(p.Name) == "" {
		page.Errors["name"] = "Ürün adını girin."
	}
	if v := strings.TrimSpace(f.Get("min_stock")); v != "" {
		q, err := store.ParseQty(v)
		if err != nil || q < 0 {
			page.Errors["min_stock"] = "Sıfır ya da pozitif bir sayı girin."
		}
		p.MinStock = q
	}
	var ok bool
	if p.WeightKg, ok = parseFloatField(f.Get("weight_kg")); !ok {
		page.Errors["weight_kg"] = "Ağırlığı sayı olarak girin, örn. 12,5."
	}
	if p.VolumeM3, ok = parseFloatField(f.Get("volume_m3")); !ok {
		page.Errors["volume_m3"] = "Hacmi sayı olarak girin, örn. 0,04."
	}
	return p
}

func (s *Server) handleProductNew(w http.ResponseWriter, r *http.Request) {
	p := s.Page(r, "Yeni ürün", "/products")
	p.Form.Set("unit", "adet")
	s.HTML(w, r, http.StatusOK, "product_form", p)
}

func (s *Server) handleProductCreate(w http.ResponseWriter, r *http.Request) {
	page := s.Page(r, "Yeni ürün", "/products")
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form okunamadı.", http.StatusBadRequest)
		return
	}
	prod := parseProduct(r, page)
	if len(page.Errors) == 0 {
		id, err := s.Store.CreateProduct(r.Context(), prod)
		switch {
		case errors.Is(err, store.ErrDuplicate):
			page.Errors["sku"] = "Bu stok kodu başka bir üründe kullanılıyor."
		case err != nil:
			s.ServerError(w, r, err)
			return
		default:
			SetFlash(w, "ok", prod.Name+" eklendi. İlk girişi aşağıdan yapabilirsiniz.")
			http.Redirect(w, r, "/products/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
			return
		}
	}
	s.HTML(w, r, http.StatusUnprocessableEntity, "product_form", page)
}

type productData struct {
	Product   store.Product
	Movements []store.Movement
	Kind      string
	Message   *Flash
	Gauge     gauge
}

// gauge positions the stock bar and the critical-level marker on a 0–400 scale.
type gauge struct {
	FillW  float64
	MarkX  float64
	HasMin bool
}

func newGauge(p store.Product) gauge {
	stock := max(p.Stock.Float(), 0)
	scale := max(stock*1.15, p.MinStock.Float()*2.5, 1)
	g := gauge{FillW: stock / scale * 400, HasMin: p.MinStock > 0}
	if g.HasMin {
		g.MarkX = p.MinStock.Float() / scale * 400
	}
	return g
}

func (s *Server) loadProductData(r *http.Request, id int64) (productData, error) {
	var d productData
	var err error
	if d.Product, err = s.Store.GetProduct(r.Context(), id); err != nil {
		return d, err
	}
	d.Movements, err = s.Store.ListMovements(r.Context(), store.MovementFilter{ProductID: id, Limit: 50})
	d.Kind = string(store.MoveIn)
	d.Gauge = newGauge(d.Product)
	return d, err
}

func (s *Server) handleProductShow(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	d, err := s.loadProductData(r, id)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, d.Product.Name, "/products")
	p.Data = d
	s.HTML(w, r, http.StatusOK, "product", p)
}

func (s *Server) handleProductEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	prod, err := s.Store.GetProduct(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, prod.Name+" düzenle", "/products")
	p.Form = productForm(prod)
	p.Data = prod
	s.HTML(w, r, http.StatusOK, "product_form", p)
}

func (s *Server) handleProductUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	current, err := s.Store.GetProduct(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form okunamadı.", http.StatusBadRequest)
		return
	}
	page := s.Page(r, current.Name+" düzenle", "/products")
	page.Data = current
	prod := parseProduct(r, page)
	prod.ID = id
	if len(page.Errors) == 0 {
		err := s.Store.UpdateProduct(r.Context(), prod)
		switch {
		case errors.Is(err, store.ErrDuplicate):
			page.Errors["sku"] = "Bu stok kodu başka bir üründe kullanılıyor."
		case err != nil:
			s.ServerError(w, r, err)
			return
		default:
			SetFlash(w, "ok", "Değişiklikler kaydedildi.")
			http.Redirect(w, r, "/products/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
			return
		}
	}
	s.HTML(w, r, http.StatusUnprocessableEntity, "product_form", page)
}

func (s *Server) handleProductArchive(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	archive := r.PostFormValue("archived") == "1"
	if err := s.Store.SetProductArchived(r.Context(), id, archive); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.NotFound(w, r)
			return
		}
		s.ServerError(w, r, err)
		return
	}
	if archive {
		SetFlash(w, "ok", "Ürün arşivlendi. Geçmiş hareketleri korunuyor.")
	} else {
		SetFlash(w, "ok", "Ürün arşivden çıkarıldı.")
	}
	Redirect(w, r, "/products/"+strconv.FormatInt(id, 10))
}

func (s *Server) handleMovementCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	kind := store.MoveKind(r.PostFormValue("kind"))
	qty, qerr := store.ParseQty(r.PostFormValue("qty"))
	var msg *Flash
	status := http.StatusOK

	if !kind.Valid() || qerr != nil {
		msg = &Flash{Kind: "error", Text: "Miktarı sayı olarak girin, örn. 12 ya da 12,5."}
		status = http.StatusUnprocessableEntity
	} else {
		_, err := s.Store.RecordMovement(r.Context(), store.MovementInput{
			ProductID: id, Kind: kind, Qty: qty, Note: r.PostFormValue("note"), UserID: UserID(r),
		})
		if err != nil {
			text := UserMessage(err)
			if text == "" {
				s.ServerError(w, r, err)
				return
			}
			msg = &Flash{Kind: "error", Text: text}
			status = http.StatusUnprocessableEntity
		} else {
			msg = &Flash{Kind: "ok", Text: kind.Label() + " kaydedildi."}
		}
	}

	if !IsHTMX(r) {
		SetFlash(w, msg.Kind, msg.Text)
		http.Redirect(w, r, "/products/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	d, err := s.loadProductData(r, id)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	d.Message = msg
	if status != http.StatusOK {
		d.Kind = string(kind)
	}
	p := s.Page(r, d.Product.Name, "/products")
	p.Data = d
	if status != http.StatusOK {
		p.Form = r.PostForm
	}
	s.Partial(w, r, status, "product", "stock-update", p)
}
