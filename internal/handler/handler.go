package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"moccha/internal/filemanager"
	"moccha/internal/system"
	"moccha/internal/terminal"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Handler struct {
	termMgr *terminal.Manager
	sysInfo *system.System
	fileMgr *filemanager.FileManager
}

func New(termMgr *terminal.Manager, sysInfo *system.System, fileMgr *filemanager.FileManager) *Handler {
	return &Handler{
		termMgr: termMgr,
		sysInfo: sysInfo,
		fileMgr: fileMgr,
	}
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	// Try to serve web UI, fallback to API info if not available
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Moccha Server</title></head>
<body>
<h1>Moccha Server Running</h1>
<p>Cloudflare Tunnel: Connected</p>
<pre>
API Endpoints:
- GET /api/health
- GET /api/system/info
- GET /api/system/processes
- GET /api/system/network
- GET /api/system/disk
- GET /api/files/*
- WebSocket: /api/terminal/ws
</pre>
<p><em>Note: Web UI not built. Run 'npm run build' in web folder.</em></p>
</body>
</html>`)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().Unix(),
		"uptime":    time.Now().Unix(),
	})
}

func (h *Handler) SystemInfo(w http.ResponseWriter, r *http.Request) {
	info, err := h.sysInfo.GetInfo(false)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

func (h *Handler) Processes(w http.ResponseWriter, r *http.Request) {
	procs, err := h.sysInfo.GetProcesses()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(procs)
}

func (h *Handler) NetworkInfo(w http.ResponseWriter, r *http.Request) {
	netInfo, err := h.sysInfo.GetNetwork()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(netInfo)
}

func (h *Handler) DiskInfo(w http.ResponseWriter, r *http.Request) {
	diskInfo, err := h.sysInfo.GetDisk()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(diskInfo)
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		path = "/"
	}

	files, err := h.fileMgr.List(path)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"path":  path,
		"root":  h.fileMgr.GetRootPath(),
		"files": files,
	})
}

func (h *Handler) CreateFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}

	isDir := r.URL.Query().Get("type") == "directory"

	err := h.fileMgr.Create(path, isDir)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"success": "created"})
}

func (h *Handler) DeleteFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}

	err := h.fileMgr.Delete(path)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"success": "deleted"})
}

func (h *Handler) RenameFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}

	var req struct {
		NewName string `json:"newName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	dir := filepath.Dir(path)
	newPath := filepath.Join(dir, req.NewName)

	err := h.fileMgr.Rename(path, newPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"success": "renamed"})
}

func (h *Handler) UploadFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}
	defer file.Close()

	filename := header.Filename
	if path != "" && !strings.HasSuffix(filepath.Base(path), "/") {
		filename = filepath.Join(filepath.Dir(path), filename)
	}

	err = h.fileMgr.Upload(filename, file)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"success": "uploaded"})
}

func (h *Handler) DownloadFile(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
		return
	}

	reader, err := h.fileMgr.Download(path)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filepath.Base(path)))
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, reader)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *Handler) TerminalWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	sessionID := uuid.New().String()
	cols := 80
	rows := 24

	if colsStr := r.URL.Query().Get("cols"); colsStr != "" {
		fmt.Sscanf(colsStr, "%d", &cols)
	}
	if rowsStr := r.URL.Query().Get("rows"); rowsStr != "" {
		fmt.Sscanf(rowsStr, "%d", &rows)
	}

	_, err = h.termMgr.CreateSession(sessionID, conn, cols, rows)
	if err != nil {
		log.Printf("Session creation error: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`{"error":"%s"}`, err)))
		return
	}

	log.Printf("Terminal session created: %s", sessionID)
}
