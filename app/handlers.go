package app

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/harunnkcnn/rampa/store"
)

// PathID parses a numeric path value.
func PathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

// UserMessage turns a store error into text safe to show, or "" if the
// error is unexpected and should be logged instead.
func UserMessage(err error) string {
	var ise *store.InsufficientStockError
	switch {
	case errors.As(err, &ise):
		return ise.Error() + "."
	case errors.Is(err, store.ErrNotFound),
		errors.Is(err, store.ErrInvalidState),
		errors.Is(err, store.ErrNoChange),
		errors.Is(err, store.ErrNonPositive),
		errors.Is(err, store.ErrEmptyShipment),
		errors.Is(err, store.ErrArchivedProduct),
		errors.Is(err, store.ErrInvalidQty),
		errors.Is(err, store.ErrMissingRequired),
		errors.Is(err, store.ErrBinNotFound),
		errors.Is(err, store.ErrUnknownCode),
		errors.Is(err, store.ErrWrongWarehouse),
		errors.Is(err, store.ErrNotOnShipment),
		errors.Is(err, store.ErrOverPick),
		errors.Is(err, store.ErrSameBin),
		errors.Is(err, store.ErrDuplicateBarcode):
		msg := []rune(err.Error())
		return strings.ToUpper(string(msg[:1])) + string(msg[1:]) + "."
	}
	return ""
}

func startOfDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

type dashboardData struct {
	Today    time.Time
	Overview store.Overview
	Low      []store.Product
	Open     []store.Shipment
	Recent   []store.Movement
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := s.Store.Now()
	sc, err := s.ScopeOf(r)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	wh := sc.ID()
	ov, err := s.Store.GetOverview(ctx, startOfDay(now), wh)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	low, err := s.Store.ListProducts(ctx, store.ProductFilter{LowOnly: true, Limit: 8, WarehouseID: wh})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	open, err := s.Store.ListShipments(ctx, store.ShipmentFilter{Open: true, Limit: 6, WarehouseID: wh})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	recent, err := s.Store.ListMovements(ctx, store.MovementFilter{Limit: 8, WarehouseID: wh})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p := s.Page(r, "Özet", "/")
	p.Data = dashboardData{Today: now, Overview: ov, Low: low, Open: open, Recent: recent}
	s.HTML(w, r, http.StatusOK, "dashboard", p)
}
