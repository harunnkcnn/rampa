package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/harunnkcnn/rampa/store"
)

type settingsData struct {
	Users []store.User
}

func (s *Server) renderSettings(w http.ResponseWriter, r *http.Request, p *Page, status int) {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	p.Data = settingsData{Users: users}
	if _, ok := p.Form["company"]; !ok {
		p.Form.Set("company", p.Company)
	}
	s.HTML(w, r, status, "settings", p)
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	s.renderSettings(w, r, s.Page(r, "Ayarlar", "/settings"), http.StatusOK)
}

func (s *Server) handleSettingsCompany(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("company"))
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	if err := s.Store.SetSetting(r.Context(), "company_name", name); err != nil {
		s.ServerError(w, r, err)
		return
	}
	SetFlash(w, "ok", "İşletme adı kaydedildi.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	ss := SessionFrom(r.Context())
	p := s.Page(r, "Ayarlar", "/settings")
	u, err := s.Store.GetUser(r.Context(), ss.User.ID)
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if ok, _ := CheckPassword(u.PasswordHash, r.PostFormValue("current")); !ok {
		p.Errors["current"] = "Mevcut parola hatalı."
	}
	validateNewPassword(p, r.PostFormValue("password"), r.PostFormValue("password2"))
	if len(p.Errors) > 0 {
		s.renderSettings(w, r, p, http.StatusUnprocessableEntity)
		return
	}
	hash, err := HashPassword(r.PostFormValue("password"))
	if err != nil {
		s.ServerError(w, r, err)
		return
	}
	if err := s.Store.SetPasswordHash(r.Context(), u.ID, hash, ss.TokenHash); err != nil {
		s.ServerError(w, r, err)
		return
	}
	SetFlash(w, "ok", "Parola değiştirildi. Diğer cihazlardaki oturumlar kapatıldı.")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *Server) handleSettingsUserCreate(w http.ResponseWriter, r *http.Request) {
	ss := SessionFrom(r.Context())
	if !ss.User.IsAdmin {
		http.Error(w, "Kullanıcı eklemek için yönetici olmalısınız.", http.StatusForbidden)
		return
	}
	p := s.Page(r, "Ayarlar", "/settings")
	username := strings.TrimSpace(r.PostFormValue("new_username"))
	p.Form.Set("new_username", username)
	if username == "" {
		p.Errors["new_username"] = "Kullanıcı adı girin."
	}
	pw := r.PostFormValue("new_password")
	if len([]rune(pw)) < MinPasswordLen {
		p.Errors["new_password"] = "Parola en az 10 karakter olmalı."
	}
	if len(p.Errors) == 0 {
		hash, err := HashPassword(pw)
		if err != nil {
			s.ServerError(w, r, err)
			return
		}
		_, err = s.Store.CreateUser(r.Context(), username, hash, r.PostFormValue("new_admin") == "1")
		switch {
		case errors.Is(err, store.ErrDuplicate):
			p.Errors["new_username"] = "Bu kullanıcı adı alınmış."
		case err != nil:
			s.ServerError(w, r, err)
			return
		default:
			SetFlash(w, "ok", username+" eklendi.")
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
			return
		}
	}
	s.renderSettings(w, r, p, http.StatusUnprocessableEntity)
}
