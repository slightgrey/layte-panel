package api

import (
	"net/http"
	"path"
	"time"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/store"
	"github.com/slightgrey/layte-panel/internal/templates"
)

// expiringSoon: certbot renews 30 days before expiry, so anything under 21
// days means renewal is failing.
const expiringSoon = 21 * 24 * time.Hour

type certJSON struct {
	actions.Certificate
	DaysLeft int    `json:"days_left"`
	State    string `json:"state"` // ok | expiring | expired
	SiteID   int64  `json:"site_id,omitempty"`
	Domain   string `json:"site_domain,omitempty"`
}

// GET /api/v1/ssl — every certificate on the server with its expiry state,
// linked to the site that uses it.
func (s *server) handleListSSL(w http.ResponseWriter, r *http.Request) {
	var certs []actions.Certificate
	if err := s.Agent.Call(r.Context(), "ssl.list", actorOf(r), nil, &certs); err != nil {
		e := s.agentError("ssl.list", err)
		writeError(w, http.StatusBadGateway, e.Code, e.Message)
		return
	}
	sites, err := s.Store.ListSites(r.Context())
	if err != nil {
		s.internalError(w, "list sites", err)
		return
	}
	// cert name → site: Layte certs by name, imported sites by their cert path.
	byName := map[string]store.Site{}
	for _, site := range sites {
		if site.LayteConfigs() {
			byName[templates.CertName(site.Domain)] = site
		}
		if !site.Managed && site.CertPath != "" { // imported/adopted: the certificate its vhost uses
			byName[path.Base(path.Dir(site.CertPath))] = site
		}
	}
	now := s.Now()
	out := make([]certJSON, 0, len(certs))
	for _, c := range certs {
		cj := certJSON{Certificate: c, DaysLeft: int(c.NotAfter.Sub(now).Hours() / 24), State: "ok"}
		switch left := c.NotAfter.Sub(now); {
		case left <= 0:
			cj.State = "expired"
		case left < expiringSoon:
			cj.State = "expiring"
		}
		if site, ok := byName[c.Name]; ok {
			cj.SiteID, cj.Domain = site.ID, site.Domain
		}
		out = append(out, cj)
	}
	email, _ := s.Store.Setting(r.Context(), store.SettingLetsEncryptEmail)
	writeJSON(w, http.StatusOK, map[string]any{"certificates": out, "email": email})
}

// POST /api/v1/sites/{id}/ssl {email, delete_old_cert} — issue a certificate and enable HTTPS.
func (s *server) handleIssueSSL(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		Email         string `json:"email"`
		DeleteOldCert bool   `json:"delete_old_cert"` // adopted sites switching from their own certificate
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.IssueSSL(r.Context(), actorOf(r), site, req.Email, req.DeleteOldCert)
	if err != nil {
		s.writeSiteError(w, "issue ssl", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// POST /api/v1/sites/{id}/ssl/renew {dry_run} — renew now, or test renewal (job).
func (s *server) handleRenewSSL(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	var req struct {
		DryRun bool `json:"dry_run"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.RenewSSL(r.Context(), actorOf(r), site, req.DryRun)
	if err != nil {
		s.writeSiteError(w, "renew ssl", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}

// DELETE /api/v1/sites/{id}/ssl — back to HTTP and revoke the certificate.
func (s *server) handleRemoveSSL(w http.ResponseWriter, r *http.Request) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return
	}
	jobID, err := s.sites.RemoveSSL(r.Context(), actorOf(r), site)
	if err != nil {
		s.writeSiteError(w, "remove ssl", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "site_id": site.ID})
}
