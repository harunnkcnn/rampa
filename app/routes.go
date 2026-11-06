package app

import (
	"io/fs"
	"net/http"

	"github.com/harunnkcnn/rampa/web"
)

func (s *Server) routes() {
	static, _ := fs.Sub(web.FS, "static")
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
	s.mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=604800")
		fileServer.ServeHTTP(w, r)
	}))
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Store.DB.PingContext(r.Context()); err != nil {
			http.Error(w, "db", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok"))
	})

	s.HandlePublic("GET /login", s.handleLoginForm)
	s.HandlePublic("POST /login", s.handleLogin)
	s.HandlePublic("POST /logout", s.handleLogout)
	s.HandlePublic("GET /setup", s.handleSetupForm)
	s.HandlePublic("POST /setup", s.handleSetup)

	s.Handle("GET /{$}", s.handleDashboard)

	s.Handle("GET /products", s.handleProducts)
	s.Handle("GET /products.csv", s.handleProductsCSV)
	s.Handle("GET /products/new", s.handleProductNew)
	s.Handle("POST /products", s.handleProductCreate)
	s.Handle("GET /products/{id}", s.handleProductShow)
	s.Handle("GET /products/{id}/edit", s.handleProductEdit)
	s.Handle("POST /products/{id}", s.handleProductUpdate)
	s.Handle("POST /products/{id}/archive", s.handleProductArchive)
	s.Handle("POST /products/{id}/movements", s.handleMovementCreate)

	s.Handle("GET /movements", s.handleMovements)
	s.Handle("GET /movements.csv", s.handleMovementsCSV)

	s.Handle("GET /shipments", s.handleShipments)
	s.Handle("POST /shipments", s.handleShipmentCreate)
	s.Handle("GET /shipments/{id}", s.handleShipmentShow)
	s.Handle("POST /shipments/{id}", s.handleShipmentUpdate)
	s.Handle("POST /shipments/{id}/items", s.handleShipmentItemAdd)
	s.Handle("POST /shipments/{id}/items/{item}/delete", s.handleShipmentItemRemove)
	s.Handle("POST /shipments/{id}/load", s.handleShipmentLoad)
	s.Handle("POST /shipments/{id}/deliver", s.handleShipmentDeliver)
	s.Handle("POST /shipments/{id}/cancel", s.handleShipmentCancel)

	s.Handle("GET /settings", s.handleSettings)
	s.Handle("POST /settings/company", s.handleSettingsCompany)
	s.Handle("POST /settings/password", s.handleSettingsPassword)
	s.Handle("POST /settings/users", s.handleSettingsUserCreate)

	s.mux.Handle("/", s.requireUser(http.HandlerFunc(s.NotFound)))

	s.AddNav(
		NavItem{Label: "Özet", Href: "/", Order: 10},
		NavItem{Label: "Ürünler", Href: "/products", Order: 20},
		NavItem{Label: "Hareketler", Href: "/movements", Order: 30},
		NavItem{Label: "Sevkiyatlar", Href: "/shipments", Order: 40},
		NavItem{Label: "Ayarlar", Href: "/settings", Order: 900, Group: "Hesap"},
	)
}
