package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Handlers struct {
	sm       *SessionManager
	registry *Registry
	baseDir  string
}

func NewHandlers(sm *SessionManager, reg *Registry, baseDir string) *Handlers {
	return &Handlers{
		sm:       sm,
		registry: reg,
		baseDir:  baseDir,
	}
}

func (h *Handlers) RegisterRoutes(mux *http.ServeMux) {
	// Public routes
	mux.HandleFunc("POST /api/login", h.handleLogin)
	mux.HandleFunc("POST /api/logout", h.handleLogout)

	// Auth-protected routes
	mux.HandleFunc("GET /api/status", h.sm.RequireAuth(h.handleStatus))
	mux.HandleFunc("GET /api/instances", h.sm.RequireAuth(h.handleInstancesList))
	mux.HandleFunc("POST /api/instances", h.sm.RequireAuth(h.handleInstanceCreate))
	mux.HandleFunc("GET /api/instances/{name}", h.sm.RequireAuth(h.handleInstanceGet))
	mux.HandleFunc("PUT /api/instances/{name}", h.sm.RequireAuth(h.handleInstanceUpdate))
	mux.HandleFunc("POST /api/instances/{name}/action", h.sm.RequireAuth(h.handleInstanceAction))
	mux.HandleFunc("DELETE /api/instances/{name}", h.sm.RequireAuth(h.handleInstanceDelete))
	mux.HandleFunc("GET /api/instances/{name}/logs", h.sm.RequireAuth(h.handleInstanceLogs))

	// Central update management routes
	mux.HandleFunc("GET /api/updates/status", h.sm.RequireAuth(handleGetUpdateStatus))
	mux.HandleFunc("POST /api/updates/check", h.sm.RequireAuth(handleCheckUpdates))
	mux.HandleFunc("POST /api/updates/trigger", h.sm.RequireAuth(handleTriggerUpdate))
	mux.HandleFunc("GET /api/updates/settings", h.sm.RequireAuth(handleGetUpdateSettings))
	mux.HandleFunc("POST /api/updates/settings", h.sm.RequireAuth(handleSaveUpdateSettings))
}

type LoginRequest struct {
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

func (h *Handlers) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request", http.StatusBadRequest)
		return
	}

	if !h.sm.VerifyPassword(req.Password) {
		writeJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := h.sm.CreateSession()
	if err != nil {
		writeJSONError(w, "Failed to create session", http.StatusInternalServerError)
		return
	}

	// Set HTTP-Only Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "pok_session",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(LoginResponse{Token: token})
}

func (h *Handlers) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("pok_session")
	if err == nil {
		h.sm.DestroySession(cookie.Value)
	}

	// Clear Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "pok_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message": "Logged out"}`))
}

func (h *Handlers) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Read max map count
	maxMapCount := "unknown"
	if data, err := os.ReadFile("/proc/sys/vm/max_map_count"); err == nil {
		maxMapCount = strings.TrimSpace(string(data))
	}

	// Read host timezone
	timezone := "UTC"
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		timezone = strings.TrimSpace(string(data))
	}

	statusMap := map[string]interface{}{
		"max_map_count": maxMapCount,
		"timezone":      timezone,
		"status":        "ok",
		"time":          time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(statusMap)
}

type InstanceListItem struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Health      string `json:"health"`
	CPU         string `json:"cpu"`
	Mem         string `json:"mem"`
	SessionName string `json:"session_name"`
	MapName     string `json:"map_name"`
	ASAPort     string `json:"asa_port"`
	RCONPort    string `json:"rcon_port"`
}

func (h *Handlers) handleInstancesList(w http.ResponseWriter, r *http.Request) {
	instances, err := ListInstances(h.baseDir)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to list instances: %s", err), http.StatusInternalServerError)
		return
	}

	list := make([]InstanceListItem, 0, len(instances))
	for _, inst := range instances {
		status, health, _ := GetContainerStatus(inst)
		cpu, mem, _ := GetContainerResourceUsage(inst)

		sessionName := ""
		mapName := ""
		asaPort := ""
		rconPort := ""

		cfg, err := LoadInstanceConfig(h.baseDir, inst)
		if err == nil {
			sessionName = cfg.Settings["Session Name"]
			mapName = cfg.Settings["Map Name"]
			asaPort = cfg.Settings["ASA Port"]
			rconPort = cfg.Settings["RCON Port"]
		}

		list = append(list, InstanceListItem{
			Name:        inst,
			Status:      status,
			Health:      health,
			CPU:         cpu,
			Mem:         mem,
			SessionName: sessionName,
			MapName:     mapName,
			ASAPort:     asaPort,
			RCONPort:    rconPort,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(list)
}

type CreateRequest struct {
	Name string `json:"name"`
}

func (h *Handlers) handleInstanceCreate(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSONError(w, "Invalid instance name", http.StatusBadRequest)
		return
	}

	// Clean instance name
	name := strings.TrimSpace(req.Name)
	name = strings.ReplaceAll(name, " ", "_")

	err := h.registry.Execute(r.Context(), "create", []string{name}, io.Discard)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-sync Discord bot servers and commands
	SyncDiscordServers()

	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"message": "Instance created"}`))
}

func (h *Handlers) handleInstanceGet(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, "Instance name required", http.StatusBadRequest)
		return
	}

	cfg, err := LoadInstanceConfig(h.baseDir, name)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to load config: %s", err), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

type UpdateConfigRequest struct {
	ImageTag string            `json:"image_tag"`
	Settings map[string]string `json:"settings"`
}

func (h *Handlers) handleInstanceUpdate(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, "Instance name required", http.StatusBadRequest)
		return
	}

	var req UpdateConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid request JSON", http.StatusBadRequest)
		return
	}

	// Validate settings are not empty
	if len(req.Settings) == 0 {
		writeJSONError(w, "Settings cannot be empty", http.StatusBadRequest)
		return
	}

	err := SaveInstanceConfig(h.baseDir, h.registry.Env.HostBaseDir, name, req.ImageTag, req.Settings)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to save configuration: %s", err), http.StatusInternalServerError)
		return
	}

	// Re-sync Discord bot servers and commands
	SyncDiscordServers()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message": "Configuration updated"}`))
}

type ActionRequest struct {
	Action string `json:"action"` // "start", "stop", "restart", "update"
}

func (h *Handlers) handleInstanceAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, "Instance name required", http.StatusBadRequest)
		return
	}

	var req ActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "Invalid action request", http.StatusBadRequest)
		return
	}

	action := strings.ToLower(req.Action)
	if action != "start" && action != "stop" && action != "restart" && action != "update" {
		writeJSONError(w, "Invalid action, must be start, stop, restart, or update", http.StatusBadRequest)
		return
	}

	err := h.registry.Execute(r.Context(), action, []string{name}, io.Discard)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"message": "Action %s initiated"}`, action)))
}

func (h *Handlers) handleInstanceLogs(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, "Instance name required", http.StatusBadRequest)
		return
	}

	// SSE configuration
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable proxy buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Writer proxy to output log lines as SSE events
	logWriter := &sseWriter{
		writer:  w,
		flusher: flusher,
	}

	ctx := r.Context()
	err := StreamLogs(ctx, name, logWriter)
	if err != nil {
		_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
	}
}

type sseWriter struct {
	writer  io.Writer
	flusher http.Flusher
}

func (s *sseWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		// Write each line as a data block in SSE format
		_, err = fmt.Fprintf(s.writer, "data: %s\n\n", line)
		if err != nil {
			return 0, err
		}
	}
	s.flusher.Flush()
	return len(p), nil
}

func writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (h *Handlers) handleInstanceDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, "Instance name required", http.StatusBadRequest)
		return
	}

	err := h.registry.Execute(r.Context(), "delete", []string{name}, io.Discard)
	if err != nil {
		writeJSONError(w, fmt.Sprintf("Failed to delete instance: %s", err), http.StatusInternalServerError)
		return
	}

	// Re-sync Discord bot servers and commands
	SyncDiscordServers()

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"message": "Instance deleted successfully"}`))
}
