package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/slightgrey/layte-panel/internal/actions"
	"github.com/slightgrey/layte-panel/internal/agent"
	"github.com/slightgrey/layte-panel/internal/fileops"
	"github.com/slightgrey/layte-panel/internal/store"
)

const maxUpload = 256 << 20 // per request (all files)

// fsBase addresses a site's folder: Layte sites by their user, imported
// sites (read-only) by their web root, adopted sites by both. ?area=data
// picks the site's non-public folder (tmp/, logs/) instead of its web files.
func fsBase(site store.Site, r *http.Request) actions.FSParams {
	p := actions.FSParams{Domain: site.Domain, Area: r.URL.Query().Get("area")}
	switch {
	case site.Managed:
		p.Username = site.SystemUser
	case site.Adopted:
		p.Username, p.WebRoot = site.SystemUser, site.RootPath
	default:
		p.WebRoot = site.RootPath
	}
	return p
}

func (s *server) fsRead(r *http.Request, p actions.FSParams) (fileops.Response, error) {
	var out fileops.Response
	err := s.Agent.Call(r.Context(), "fs.read", actorOf(r), actions.FSReadParams(p), &out)
	return out, err
}

func (s *server) fsWrite(r *http.Request, p actions.FSParams) (fileops.Response, error) {
	var out fileops.Response
	err := s.Agent.Call(r.Context(), "fs.write", actorOf(r), actions.FSWriteParams(p), &out)
	return out, err
}

// fsSite loads the site for a file request and refuses writes on imported sites.
func (s *server) fsSite(w http.ResponseWriter, r *http.Request, write bool) (store.Site, bool) {
	site, ok := s.loadSite(w, r)
	if !ok {
		return site, false
	}
	if (site.Managed || site.Adopted) && site.Status != "active" {
		writeError(w, http.StatusConflict, "site_busy", "the site is "+site.Status)
		return site, false
	}
	if write && !site.Managed && !site.Adopted {
		writeError(w, http.StatusConflict, "site_unmanaged", "imported sites are read-only in Layte Panel")
		return site, false
	}
	if !site.Managed && site.RootPath == "" {
		writeError(w, http.StatusNotFound, "not_found", "this site has no web root")
		return site, false
	}
	return site, true
}

func (s *server) writeFSError(w http.ResponseWriter, what string, err error) {
	e := s.agentError(what, err)
	status := http.StatusBadGateway
	switch e.Code {
	case "fs_error", "invalid_params":
		status = http.StatusBadRequest
		e.Message = agentMessage(err)
	case "not_found":
		status = http.StatusNotFound
	}
	writeError(w, status, e.Code, e.Message)
}

// GET /api/v1/sites/{id}/files?path=
func (s *server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	site, ok := s.fsSite(w, r, false)
	if !ok {
		return
	}
	p := fsBase(site, r)
	p.Op, p.Path = "list", r.URL.Query().Get("path")
	res, err := s.fsRead(r, p)
	if err != nil {
		s.writeFSError(w, "list files", err)
		return
	}
	if res.Entries == nil {
		res.Entries = []fileops.Entry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": res.Entries, "read_only": !site.Managed && !site.Adopted, "root": fsRootLabel(site, p.Area)})
}

// fsRootLabel is the folder the file manager shows paths relative to.
func fsRootLabel(site store.Site, area string) string {
	switch {
	case site.Managed:
		return "/home/" + site.SystemUser + "/" + site.Domain
	case site.Adopted && area == "data":
		return "/home/" + site.SystemUser
	}
	return site.RootPath
}

// GET /api/v1/sites/{id}/files/content?path= — a text file for the editor.
func (s *server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	site, ok := s.fsSite(w, r, false)
	if !ok {
		return
	}
	p := fsBase(site, r)
	p.Path = r.URL.Query().Get("path")
	p.Op = "stat"
	st, err := s.fsRead(r, p)
	if err != nil {
		s.writeFSError(w, "stat file", err)
		return
	}
	if st.Entry == nil || st.Entry.Type != "file" {
		writeError(w, http.StatusBadRequest, "not_a_file", "not a regular file")
		return
	}
	if st.Size > fileops.MaxEditSize {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "files over 2 MB can't be edited here; download it instead")
		return
	}
	var content []byte
	for off := int64(0); off < st.Size; {
		p.Op, p.Offset, p.Length = "read", off, fileops.MaxChunk
		res, err := s.fsRead(r, p)
		if err != nil {
			s.writeFSError(w, "read file", err)
			return
		}
		if len(res.Data) == 0 {
			break
		}
		content = append(content, res.Data...)
		off += int64(len(res.Data))
	}
	if !utf8.Valid(content) {
		writeError(w, http.StatusUnsupportedMediaType, "binary", "this looks like a binary file; download it instead")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"content": string(content), "size": st.Size, "read_only": !site.Managed})
}

// writeAll sends data to a file in chunks; the file is replaced only after
// the last chunk arrives.
func (s *server) writeAll(r *http.Request, base actions.FSParams, name string, src io.Reader) (int64, error) {
	buf := make([]byte, fileops.MaxChunk)
	var off int64
	for {
		n, err := io.ReadFull(src, buf)
		if n > 0 {
			p := base
			p.Op, p.Path, p.Offset, p.Chunk = "write", name, off, buf[:n]
			if _, werr := s.fsWrite(r, p); werr != nil {
				return off, werr
			}
			off += int64(n)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return off, err
		}
	}
	p := base
	p.Op, p.Path, p.Offset, p.Final = "write", name, off, true
	_, err := s.fsWrite(r, p)
	return off, err
}

// PUT /api/v1/sites/{id}/files/content?path= {content}
func (s *server) handleSaveFile(w http.ResponseWriter, r *http.Request) {
	site, ok := s.fsSite(w, r, true)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 3*fileops.MaxEditSize)
	var req struct {
		Content string `json:"content"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body (or file over 2 MB)")
		return
	}
	if len(req.Content) > fileops.MaxEditSize {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large", "files over 2 MB can't be saved from the editor")
		return
	}
	name := r.URL.Query().Get("path")
	n, err := s.writeAll(r, fsBase(site, r), name, strings.NewReader(req.Content))
	if err != nil {
		s.writeFSError(w, "save file", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"size": n})
}

// POST /api/v1/sites/{id}/files/upload?path=<dir> (multipart, field "files")
func (s *server) handleUpload(w http.ResponseWriter, r *http.Request) {
	site, ok := s.fsSite(w, r, true)
	if !ok {
		return
	}
	dir, err := fileops.CleanPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "expected a multipart upload")
		return
	}
	var uploaded []string
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "upload interrupted or larger than 256 MB")
			return
		}
		name := path.Base(part.FileName())
		if part.FormName() != "files" || name == "" || name == "." || name == "/" {
			part.Close()
			continue
		}
		if _, err := s.writeAll(r, fsBase(site, r), path.Join(dir, name), part); err != nil {
			part.Close()
			s.writeFSError(w, "upload", err)
			return
		}
		part.Close()
		uploaded = append(uploaded, name)
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded})
}

// GET /api/v1/sites/{id}/files/download?path=
func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	site, ok := s.fsSite(w, r, false)
	if !ok {
		return
	}
	p := fsBase(site, r)
	p.Op, p.Path = "stat", r.URL.Query().Get("path")
	st, err := s.fsRead(r, p)
	if err != nil {
		s.writeFSError(w, "stat file", err)
		return
	}
	if st.Entry == nil || st.Entry.Type != "file" {
		writeError(w, http.StatusBadRequest, "not_a_file", "only files can be downloaded")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": st.Entry.Name}))
	h.Set("Content-Length", strconv.FormatInt(st.Size, 10))
	h.Set("Cache-Control", "no-store")
	for off := int64(0); off < st.Size; {
		p.Op, p.Offset, p.Length = "read", off, fileops.MaxChunk
		res, err := s.fsRead(r, p)
		if err != nil || len(res.Data) == 0 {
			return // headers are sent; the client sees a short download
		}
		if _, err := w.Write(res.Data); err != nil {
			return
		}
		off += int64(len(res.Data))
	}
}

// POST /api/v1/sites/{id}/files/{op} — mkdir {path} | rename {path,to} | delete {path}
func (s *server) handleFileOp(w http.ResponseWriter, r *http.Request) {
	op := r.PathValue("op")
	if op != "mkdir" && op != "rename" && op != "delete" {
		writeError(w, http.StatusNotFound, "not_found", "no such operation")
		return
	}
	site, ok := s.fsSite(w, r, true)
	if !ok {
		return
	}
	var req struct {
		Path string `json:"path"`
		To   string `json:"to"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	p := fsBase(site, r)
	p.Op, p.Path, p.To = op, req.Path, req.To
	if _, err := s.fsWrite(r, p); err != nil {
		s.writeFSError(w, op, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// agentMessage returns the agent's own (user-safe) error message.
func agentMessage(err error) string {
	var ae *agent.Error
	if errors.As(err, &ae) {
		return ae.Message
	}
	return err.Error()
}
