package api

import (
	"net/http"

	"github.com/slightgrey/layte-panel/internal/sites"
	"github.com/slightgrey/layte-panel/internal/store"
)

// sftpSite loads the {id} site's SFTP holder: the site, or for a subdomain
// site its main site (one login per Linux user).
func (s *server) sftpSite(w http.ResponseWriter, r *http.Request) (store.Site, bool) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return site, false
	}
	main, err := s.sites.SFTPSite(r.Context(), site)
	if err != nil {
		s.internalError(w, "load main site", err)
		return site, false
	}
	return main, true
}

// GET /api/v1/sites/{id}/sftp — the site's SFTP login (keys, password option).
func (s *server) handleSFTP(w http.ResponseWriter, r *http.Request) {
	site, ok := s.sftpSite(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.sites.SFTP(r.Context(), site))
}

// PUT /api/v1/sites/{id}/sftp {enabled, password, keys} — turn the login on
// or off, or change its keys and password option.
func (s *server) handleSetSFTP(w http.ResponseWriter, r *http.Request) {
	site, ok := s.sftpSite(w, r)
	if !ok {
		return
	}
	var req sites.SFTPRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	st, err := s.sites.SetSFTP(r.Context(), actorOf(r), site, req)
	if err != nil {
		s.writeSiteError(w, "sftp", err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// POST /api/v1/sites/{id}/sftp/password — a new password, shown once.
func (s *server) handleSFTPPassword(w http.ResponseWriter, r *http.Request) {
	site, ok := s.sftpSite(w, r)
	if !ok {
		return
	}
	pw, err := s.sites.NewSFTPPassword(r.Context(), actorOf(r), site)
	if err != nil {
		s.writeSiteError(w, "sftp password", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"password": pw})
}
