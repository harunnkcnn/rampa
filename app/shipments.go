package app

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/harunnkcnn/rampa/store"
)

type shipmentsData struct {
	Shipments []store.Shipment
	Tab       string
}

func (s *Server) handleShipments(w http.ResponseWriter, r *http.Request) {
	tab := r.URL.Query().Get("tab")
	f := store.ShipmentFilter{Limit: 200}
	switch tab {
	case "delivered":
		f.Status = store.ShipDelivered
	case "cancelled":
		f.Status = store.ShipCancelled
	case "all":
	default:
		tab = "open"
		f.Open = true
	}
	list, err := s.Store.ListShipments(r.Context(), f)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, "Sevkiyatlar", "/shipments")
	p.Data = shipmentsData{Shipments: list, Tab: tab}
	s.HTML(w, r, http.StatusOK, "shipments", p)
}

func headerFromForm(r *http.Request) store.ShipmentHeader {
	return store.ShipmentHeader{
		Customer: r.PostFormValue("customer"),
		Plate:    r.PostFormValue("plate"),
		Driver:   r.PostFormValue("driver"),
		Note:     r.PostFormValue("note"),
	}
}

func (s *Server) handleShipmentCreate(w http.ResponseWriter, r *http.Request) {
	id, err := s.Store.CreateShipment(r.Context(), headerFromForm(r), UserID(r))
	if errors.Is(err, store.ErrMissingRequired) {
		SetFlash(w, "error", "Sevkiyat için müşteri ya da alıcı adı girin.")
		http.Redirect(w, r, "/shipments", http.StatusSeeOther)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, shipmentURL(id), http.StatusSeeOther)
}

func shipmentURL(id int64) string { return "/shipments/" + strconv.FormatInt(id, 10) }

type shipmentData struct {
	Shipment store.Shipment
	Products []store.Product
	Message  *Flash
	// Extras lets extensions append panels (e.g. vehicle fit) to the page.
	Extras []ShipmentPanel
}

// ShipmentPanel is extra HTML rendered on the shipment page by an extension.
type ShipmentPanel struct {
	Title string
	Body  any // template.HTML
}

// ShipmentPanelFunc builds extension panels for a shipment page.
type ShipmentPanelFunc func(r *http.Request, sh store.Shipment) (ShipmentPanel, bool)

// AddShipmentPanel registers a panel provider for the shipment page.
func (s *Server) AddShipmentPanel(fn ShipmentPanelFunc) { s.panels = append(s.panels, fn) }

func (s *Server) loadShipmentData(r *http.Request, id int64) (shipmentData, error) {
	var d shipmentData
	var err error
	if d.Shipment, err = s.Store.GetShipment(r.Context(), id); err != nil {
		return d, err
	}
	if d.Shipment.Editable() {
		if d.Products, err = s.Store.ListProducts(r.Context(), store.ProductFilter{}); err != nil {
			return d, err
		}
	}
	for _, fn := range s.panels {
		if p, ok := fn(r, d.Shipment); ok {
			d.Extras = append(d.Extras, p)
		}
	}
	return d, nil
}

func (s *Server) renderShipment(w http.ResponseWriter, r *http.Request, id int64, msg *Flash, status int) {
	d, err := s.loadShipmentData(r, id)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	d.Message = msg
	p := s.Page(r, d.Shipment.Code, "/shipments")
	p.Data = d
	if IsHTMX(r) && r.Header.Get("HX-Target") == "shipment-items" {
		s.Partial(w, r, status, "shipment", "shipment-items", p)
		return
	}
	s.HTML(w, r, status, "shipment", p)
}

func (s *Server) handleShipmentShow(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	s.renderShipment(w, r, id, nil, http.StatusOK)
}

func (s *Server) handleShipmentUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	err := s.Store.UpdateShipmentHeader(r.Context(), id, headerFromForm(r))
	switch {
	case errors.Is(err, store.ErrMissingRequired):
		SetFlash(w, "error", "Müşteri ya da alıcı adı boş bırakılamaz.")
	case err != nil && UserMessage(err) != "":
		SetFlash(w, "error", UserMessage(err))
	case err != nil:
		s.ServerError(w, r, err)
		return
	default:
		SetFlash(w, "ok", "Sevkiyat bilgileri kaydedildi.")
	}
	http.Redirect(w, r, shipmentURL(id), http.StatusSeeOther)
}

func (s *Server) itemResult(w http.ResponseWriter, r *http.Request, id int64, err error, okText string) {
	msg := &Flash{Kind: "ok", Text: okText}
	status := http.StatusOK
	if err != nil {
		text := UserMessage(err)
		if text == "" {
			s.ServerError(w, r, err)
			return
		}
		msg = &Flash{Kind: "error", Text: text}
		status = http.StatusUnprocessableEntity
	}
	if !IsHTMX(r) {
		SetFlash(w, msg.Kind, msg.Text)
		http.Redirect(w, r, shipmentURL(id), http.StatusSeeOther)
		return
	}
	s.renderShipment(w, r, id, msg, status)
}

func (s *Server) handleShipmentItemAdd(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	pid, _ := strconv.ParseInt(r.PostFormValue("product_id"), 10, 64)
	qty, err := store.ParseQty(r.PostFormValue("qty"))
	if err == nil && pid == 0 {
		err = store.ErrMissingRequired
	}
	if err == nil {
		err = s.Store.AddShipmentItem(r.Context(), id, pid, qty)
	}
	s.itemResult(w, r, id, err, "Ürün sevkiyata eklendi.")
}

func (s *Server) handleShipmentItemRemove(w http.ResponseWriter, r *http.Request) {
	id, ok1 := PathID(r, "id")
	item, ok2 := PathID(r, "item")
	if !ok1 || !ok2 {
		s.NotFound(w, r)
		return
	}
	s.itemResult(w, r, id, s.Store.RemoveShipmentItem(r.Context(), id, item), "Satır çıkarıldı.")
}

func (s *Server) transition(w http.ResponseWriter, r *http.Request, fn func(id int64) error, okText string) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	if err := fn(id); err != nil {
		text := UserMessage(err)
		if text == "" {
			s.ServerError(w, r, err)
			return
		}
		SetFlash(w, "error", text)
	} else {
		SetFlash(w, "ok", okText)
	}
	Redirect(w, r, shipmentURL(id))
}

func (s *Server) handleShipmentLoad(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, func(id int64) error {
		return s.Store.LoadShipment(r.Context(), id, UserID(r))
	}, "Sevkiyat yüklendi, ürünler stoktan düşüldü.")
}

func (s *Server) handleShipmentDeliver(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, func(id int64) error {
		return s.Store.DeliverShipment(r.Context(), id)
	}, "Sevkiyat teslim edildi olarak kapatıldı.")
}

func (s *Server) handleShipmentCancel(w http.ResponseWriter, r *http.Request) {
	s.transition(w, r, func(id int64) error {
		return s.Store.CancelShipment(r.Context(), id, UserID(r))
	}, "Sevkiyat iptal edildi.")
}
