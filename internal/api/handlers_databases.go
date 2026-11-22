package api

import (
	"net/http"
)

// GET /api/v1/databases
func (s *server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	engine, dbs, err := s.sites.ListDatabases(r.Context(), actorOf(r))
	if err != nil {
		s.writeSiteError(w, "list databases", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"engine": engine, "databases": dbs})
}

// POST /api/v1/databases {name, site_id?} — 201 with the password (shown once).
func (s *server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name   string `json:"name"`
		SiteID int64  `json:"site_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	pw, err := s.sites.CreateDatabase(r.Context(), actorOf(r), req.Name, req.SiteID)
	if err != nil {
		s.writeSiteError(w, "create database", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{"secrets": map[string]string{"db_name": req.Name, "db_user": req.Name, "db_password": pw}})
}

// DELETE /api/v1/databases/{name} {confirm}
func (s *server) handleDropDatabase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.sites.DropDatabase(r.Context(), actorOf(r), r.PathValue("name"), req.Confirm); err != nil {
		s.writeSiteError(w, "drop database", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/databases/{name}/password — new random password (shown once).
func (s *server) handleResetDBPassword(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	pw, err := s.sites.ResetDBPassword(r.Context(), actorOf(r), name)
	if err != nil {
		s.writeSiteError(w, "reset database password", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"secrets": map[string]string{"db_user": name, "db_password": pw}})
}

// POST /api/v1/databases/{name}/export — dump to the site's tmp/ folder (job).
func (s *server) handleExportDatabase(w http.ResponseWriter, r *http.Request) {
	jobID, err := s.sites.ExportDatabase(r.Context(), actorOf(r), r.PathValue("name"))
	if err != nil {
		s.writeSiteError(w, "export database", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}

// POST /api/v1/databases/{name}/import {path, confirm} — run tmp/<file>.sql (job).
func (s *server) handleImportDatabase(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		Confirm string `json:"confirm"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	jobID, err := s.sites.ImportDatabase(r.Context(), actorOf(r), r.PathValue("name"), req.Path, req.Confirm)
	if err != nil {
		s.writeSiteError(w, "import database", err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": jobID})
}
