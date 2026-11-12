package app

import (
	"context"
	"net/http"
	"strconv"

	"github.com/harunnkcnn/rampa/store"
)

// Scope is the warehouse a signed-in user is working in.
type Scope struct {
	Current    *store.Warehouse  // nil means all warehouses (admins only)
	Warehouses []store.Warehouse // the warehouses this user may use
	CanAll     bool
}

// ID is the warehouse id, 0 for all. A user without any warehouse gets -1,
// which matches nothing.
func (s Scope) ID() int64 {
	if s.Current != nil {
		return s.Current.ID
	}
	if s.CanAll {
		return 0
	}
	return -1
}

// Multi reports whether choosing a warehouse makes sense.
func (s Scope) Multi() bool { return len(s.Warehouses) > 1 }

// Label names the scope for headings.
func (s Scope) Label() string {
	if s.Current != nil {
		return s.Current.Label()
	}
	return "Tüm depolar"
}

// Allows reports whether the scope's user may work in warehouseID.
func (s Scope) Allows(warehouseID int64) bool {
	for _, w := range s.Warehouses {
		if w.ID == warehouseID {
			return true
		}
	}
	return false
}

type scopeKey struct{}

// ScopeOf resolves the session's warehouse, cached per request.
func (s *Server) ScopeOf(r *http.Request) (Scope, error) {
	if sc, ok := r.Context().Value(scopeKey{}).(*Scope); ok && sc != nil {
		return *sc, nil
	}
	ss := SessionFrom(r.Context())
	if ss == nil {
		return Scope{}, nil
	}
	ws, err := s.Store.UserWarehouses(r.Context(), ss.User.ID, ss.User.IsAdmin)
	if err != nil {
		return Scope{}, err
	}
	sc := Scope{Warehouses: ws, CanAll: ss.User.IsAdmin}
	for i := range ws {
		if ws[i].ID == ss.WarehouseID {
			sc.Current = &ws[i]
		}
	}
	if sc.Current == nil && !sc.CanAll && len(ws) > 0 {
		sc.Current = &ws[0]
	}
	if sc.Current == nil && len(ws) == 1 {
		sc.Current = &ws[0]
	}
	return sc, nil
}

// withScopeCache lets ScopeOf compute once per request.
func (s *Server) withScopeCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) != nil {
			if sc, err := s.ScopeOf(r); err == nil {
				r = r.WithContext(context.WithValue(r.Context(), scopeKey{}, &sc))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleScope(w http.ResponseWriter, r *http.Request) {
	ss := SessionFrom(r.Context())
	sc, err := s.ScopeOf(r)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	id, _ := strconv.ParseInt(r.PostFormValue("warehouse_id"), 10, 64)
	if !(id == 0 && sc.CanAll) && !sc.Allows(id) {
		http.Error(w, "Bu depoya erişiminiz yok.", http.StatusForbidden)
		return
	}
	if err := s.Store.SetSessionWarehouse(r.Context(), ss.TokenHash, id); err != nil {
		s.ServerError(w, r, err)
		return
	}
	http.Redirect(w, r, safeNext(r.PostFormValue("back")), http.StatusSeeOther)
}

// binChoices lists the bins a form may book into for the current scope,
// with the GENEL bin of the current warehouse preselected.
func (s *Server) binChoices(r *http.Request) ([]store.Bin, int64, error) {
	sc, err := s.ScopeOf(r)
	if err != nil {
		return nil, 0, err
	}
	var out []store.Bin
	for _, w := range sc.Warehouses {
		if sc.Current != nil && w.ID != sc.Current.ID {
			continue
		}
		bins, err := s.Store.ListBins(r.Context(), w.ID)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, bins...)
	}
	var def int64
	if len(out) > 0 {
		def = out[0].ID
	}
	return out, def, nil
}
