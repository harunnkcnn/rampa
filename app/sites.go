package app

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/harunnkcnn/rampa/store"
)

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if ss := SessionFrom(r.Context()); ss == nil || !ss.User.IsAdmin {
			http.Error(w, "Bu sayfa yöneticiler içindir.", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

type warehousesData struct {
	Branches []store.Branch
	Bins     map[int64][]store.Bin
	Open     int64 // warehouse whose bin list is expanded
}

func (s *Server) renderWarehouses(w http.ResponseWriter, r *http.Request, p *Page, status int) {
	branches, err := s.Store.ListBranches(r.Context())
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	d := warehousesData{Branches: branches, Bins: map[int64][]store.Bin{}}
	d.Open, _ = strconv.ParseInt(r.URL.Query().Get("depo"), 10, 64)
	for _, b := range branches {
		for _, wh := range b.Warehouses {
			bins, err := s.Store.ListBins(r.Context(), wh.ID)
			if err != nil {
				s.ServerError(w, r, err)
				return
			}
			d.Bins[wh.ID] = bins
		}
	}
	p.Data = d
	s.HTML(w, r, status, "warehouses", p)
}

func (s *Server) handleWarehouses(w http.ResponseWriter, r *http.Request) {
	s.renderWarehouses(w, r, s.Page(r, "Depolar ve hücreler", "/settings/warehouses"), http.StatusOK)
}

func (s *Server) handleBranchCreate(w http.ResponseWriter, r *http.Request) {
	p := s.Page(r, "Depolar ve hücreler", "/settings/warehouses")
	p.Form = r.PostForm
	_, err := s.Store.CreateBranch(r.Context(), r.PostFormValue("branch_name"))
	switch {
	case errors.Is(err, store.ErrMissingRequired):
		p.Errors["branch_name"] = "Şube adını girin."
	case errors.Is(err, store.ErrDuplicate):
		p.Errors["branch_name"] = "Bu adla bir şube zaten var."
	case err != nil:
		s.ServerError(w, r, err)
		return
	default:
		SetFlash(w, "ok", "Şube eklendi. Şimdi altına depo ekleyin.")
		http.Redirect(w, r, "/settings/warehouses", http.StatusSeeOther)
		return
	}
	s.renderWarehouses(w, r, p, http.StatusUnprocessableEntity)
}

func (s *Server) handleWarehouseCreate(w http.ResponseWriter, r *http.Request) {
	p := s.Page(r, "Depolar ve hücreler", "/settings/warehouses")
	p.Form = r.PostForm
	branchID, _ := strconv.ParseInt(r.PostFormValue("branch_id"), 10, 64)
	id, err := s.Store.CreateWarehouse(r.Context(), branchID, r.PostFormValue("code"), r.PostFormValue("name"))
	switch {
	case errors.Is(err, store.ErrMissingRequired):
		p.Errors["wh"] = "Şube, depo kodu ve depo adı gerekli."
	case errors.Is(err, store.ErrDuplicate):
		p.Errors["wh"] = "Bu depo kodu kullanılıyor."
	case err != nil:
		s.ServerError(w, r, err)
		return
	default:
		SetFlash(w, "ok", "Depo eklendi. GENEL hücresi otomatik oluşturuldu; raf hücrelerini aşağıdan ekleyin.")
		http.Redirect(w, r, "/settings/warehouses?depo="+strconv.FormatInt(id, 10)+"#depo-"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	s.renderWarehouses(w, r, p, http.StatusUnprocessableEntity)
}

var rangeRe = regexp.MustCompile(`^(.*?)(\d+)\s*\.\.\s*(\d+)$`)

// ExpandBinCodes turns lines like "A-01-1..6" into A-01-1 … A-01-6. Other
// lines (and commas) are taken as single codes. At most 500 codes.
func ExpandBinCodes(text string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ',' || r == ';' }) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := rangeRe.FindStringSubmatch(line); m != nil {
			from, _ := strconv.Atoi(m[2])
			to, _ := strconv.Atoi(m[3])
			width := len(m[2])
			if to >= from && to-from < 500 {
				for i := from; i <= to && len(out) < 500; i++ {
					n := strconv.Itoa(i)
					for len(n) < width {
						n = "0" + n
					}
					out = append(out, m[1]+n)
				}
				continue
			}
		}
		if len(out) < 500 {
			out = append(out, line)
		}
	}
	return out
}

func (s *Server) handleBinsCreate(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	codes := ExpandBinCodes(r.PostFormValue("codes"))
	n, err := s.Store.CreateBins(r.Context(), id, codes)
	if errors.Is(err, store.ErrNotFound) {
		s.NotFound(w, r)
		return
	}
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	switch {
	case len(codes) == 0:
		SetFlash(w, "error", "Hücre kodu girin, örn. A-01-1..6")
	case n == 0:
		SetFlash(w, "info", "Bu hücrelerin hepsi zaten tanımlı.")
	default:
		SetFlash(w, "ok", strconv.Itoa(n)+" hücre eklendi.")
	}
	http.Redirect(w, r, "/settings/warehouses?depo="+strconv.FormatInt(id, 10)+"#depo-"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) handleWarehouseActive(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	active := r.PostFormValue("active") == "1"
	if err := s.Store.SetWarehouseActive(r.Context(), id, active); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.NotFound(w, r)
			return
		}
		s.ServerError(w, r, err)
		return
	}
	if active {
		SetFlash(w, "ok", "Depo yeniden kullanımda.")
	} else {
		SetFlash(w, "ok", "Depo pasife alındı. Stok kayıtları korunuyor.")
	}
	http.Redirect(w, r, "/settings/warehouses", http.StatusSeeOther)
}

// PIN rules for handheld sign-in: 4 to 8 digits.
var pinRe = regexp.MustCompile(`^\d{4,8}$`)

func (s *Server) handleUserPIN(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	pin := strings.TrimSpace(r.PostFormValue("pin"))
	hash := ""
	if pin != "" {
		if !pinRe.MatchString(pin) {
			SetFlash(w, "error", "PIN 4 ile 8 hane arasında, yalnızca rakam olmalı.")
			http.Redirect(w, r, "/settings#users", http.StatusSeeOther)
			return
		}
		var err error
		if hash, err = HashPassword(pin); err != nil {
			s.ServerError(w, r, err)
			return
		}
	}
	if err := s.Store.SetPINHash(r.Context(), id, hash); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.NotFound(w, r)
			return
		}
		s.ServerError(w, r, err)
		return
	}
	if hash == "" {
		SetFlash(w, "ok", "Terminal PIN'i kaldırıldı.")
	} else {
		SetFlash(w, "ok", "Terminal PIN'i kaydedildi.")
	}
	http.Redirect(w, r, "/settings#users", http.StatusSeeOther)
}

func (s *Server) handleUserWarehouses(w http.ResponseWriter, r *http.Request) {
	id, ok := PathID(r, "id")
	if !ok {
		s.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Form okunamadı.", http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, v := range r.PostForm["warehouse"] {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			ids = append(ids, n)
		}
	}
	if err := s.Store.SetUserWarehouses(r.Context(), id, ids); err != nil {
		s.ServerError(w, r, err)
		return
	}
	SetFlash(w, "ok", "Depo yetkileri kaydedildi.")
	http.Redirect(w, r, "/settings#users", http.StatusSeeOther)
}
