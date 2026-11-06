package app

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"time"

	"github.com/harunnkcnn/rampa/store"
)

const movementsPageSize = 100

type movementsData struct {
	Movements []store.Movement
	Kind      string
	From      string
	To        string
	Offset    int
	NextPage  int
	PrevPage  int
	HasMore   bool
}

func parseMovementFilter(r *http.Request) (store.MovementFilter, movementsData) {
	q := r.URL.Query()
	d := movementsData{Kind: q.Get("kind"), From: q.Get("from"), To: q.Get("to")}
	f := store.MovementFilter{Kind: store.MoveKind(d.Kind)}
	if t, err := time.ParseInLocation("2006-01-02", d.From, time.Local); err == nil {
		f.Since = t
	} else {
		d.From = ""
	}
	if t, err := time.ParseInLocation("2006-01-02", d.To, time.Local); err == nil {
		f.Until = t.AddDate(0, 0, 1)
	} else {
		d.To = ""
	}
	d.Offset, _ = strconv.Atoi(q.Get("offset"))
	if d.Offset < 0 {
		d.Offset = 0
	}
	return f, d
}

func (s *Server) handleMovements(w http.ResponseWriter, r *http.Request) {
	f, d := parseMovementFilter(r)
	f.Limit = movementsPageSize + 1
	f.Offset = d.Offset
	ms, err := s.Store.ListMovements(r.Context(), f)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if len(ms) > movementsPageSize {
		d.HasMore = true
		ms = ms[:movementsPageSize]
	}
	d.Movements = ms
	d.NextPage = d.Offset + movementsPageSize
	d.PrevPage = max(0, d.Offset-movementsPageSize)
	p := s.Page(r, "Hareketler", "/movements")
	p.Data = d
	s.HTML(w, r, http.StatusOK, "movements", p)
}

// CSVWriter returns a writer that Excel in Turkish locale opens correctly:
// UTF-8 BOM and semicolon separators.
func CSVWriter(w http.ResponseWriter, filename string) *csv.Writer {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Write([]byte{0xEF, 0xBB, 0xBF}) // UTF-8 BOM
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	return cw
}

// CSVSafe neutralises spreadsheet formula injection in user-entered text.
func CSVSafe(v string) string {
	if v != "" && (v[0] == '=' || v[0] == '+' || v[0] == '-' || v[0] == '@' || v[0] == '\t' || v[0] == '\r') {
		return "'" + v
	}
	return v
}

func (s *Server) handleMovementsCSV(w http.ResponseWriter, r *http.Request) {
	f, _ := parseMovementFilter(r)
	ms, err := s.Store.ListMovements(r.Context(), f)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	cw := CSVWriter(w, "rampa-hareketler-"+time.Now().Format("2006-01-02")+".csv")
	cw.Write([]string{"Tarih", "Stok kodu", "Ürün", "Tür", "Miktar", "Birim", "Sevkiyat", "Not", "Kullanıcı"})
	for _, m := range ms {
		cw.Write([]string{
			m.CreatedAt.Format("02.01.2006 15:04"), CSVSafe(m.ProductSKU), CSVSafe(m.ProductName),
			m.Kind.Label(), m.Qty.Input(), CSVSafe(m.Unit), m.ShipmentCode, CSVSafe(m.Note), CSVSafe(m.UserName),
		})
	}
	cw.Flush()
}

func (s *Server) handleProductsCSV(w http.ResponseWriter, r *http.Request) {
	ps, err := s.Store.ListProducts(r.Context(), store.ProductFilter{})
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	cw := CSVWriter(w, "rampa-stok-"+time.Now().Format("2006-01-02")+".csv")
	cw.Write([]string{"Stok kodu", "Ürün", "Konum", "Stok", "Birim", "Kritik seviye", "Birim ağırlık (kg)", "Birim hacim (m3)"})
	for _, p := range ps {
		cw.Write([]string{
			CSVSafe(p.SKU), CSVSafe(p.Name), CSVSafe(p.Location), p.Stock.Input(), CSVSafe(p.Unit),
			p.MinStock.Input(), floatInput(p.WeightKg), floatInput(p.VolumeM3),
		})
	}
	cw.Flush()
}
