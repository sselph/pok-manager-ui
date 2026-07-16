package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SessionManager keeps track of valid sessions in memory.
type SessionManager struct {
	mu            sync.Mutex
	sessions      map[string]time.Time
	adminPassword string
	passwordFile  string
}

// NewSessionManager creates and initializes a new SessionManager.
func NewSessionManager(baseDir string) *SessionManager {
	sm := &SessionManager{
		sessions:     make(map[string]time.Time),
		passwordFile: filepath.Join(baseDir, "config", "POK-manager", "web_password.txt"),
	}
	sm.initPassword()
	return sm
}

// initPassword reads the password from:
// 1. config/POK-manager/web_password.txt
// 2. POK_ADMIN_PASSWORD environment variable
// 3. Defaults to "admin" and saves it to web_password.txt
func (sm *SessionManager) initPassword() {
	// 1. Try file
	if data, err := os.ReadFile(sm.passwordFile); err == nil {
		sm.adminPassword = strings.TrimSpace(string(data))
		if sm.adminPassword != "" {
			return
		}
	}

	// 2. Try Env
	envPass := os.Getenv("POK_ADMIN_PASSWORD")
	if envPass != "" {
		sm.adminPassword = envPass
		// Save it to file for persistence
		_ = os.MkdirAll(filepath.Dir(sm.passwordFile), 0755)
		_ = os.WriteFile(sm.passwordFile, []byte(envPass), 0600)
		return
	}

	// 3. Default
	sm.adminPassword = "admin"
	_ = os.MkdirAll(filepath.Dir(sm.passwordFile), 0755)
	_ = os.WriteFile(sm.passwordFile, []byte("admin"), 0600)
}

// VerifyPassword checks if the provided password matches the admin password.
func (sm *SessionManager) VerifyPassword(password string) bool {
	return sm.adminPassword == password
}

// CreateSession generates a new random session token, registers it in memory, and returns it.
func (sm *SessionManager) CreateSession() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(bytes)

	sm.mu.Lock()
	sm.sessions[token] = time.Now().Add(24 * time.Hour) // Valid for 24h
	sm.mu.Unlock()

	return token, nil
}

// ValidateSession checks if a session token is valid and not expired, extending its expiry on activity.
func (sm *SessionManager) ValidateSession(token string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	expiry, exists := sm.sessions[token]
	if !exists {
		return false
	}

	if time.Now().After(expiry) {
		delete(sm.sessions, token)
		return false
	}

	// Extend session duration on activity
	sm.sessions[token] = time.Now().Add(24 * time.Hour)
	return true
}

// DestroySession invalidates and removes the given session token from the manager.
func (sm *SessionManager) DestroySession(token string) {
	sm.mu.Lock()
	delete(sm.sessions, token)
	sm.mu.Unlock()
}

// RequireAuth is a middleware that protects HTTP endpoints, validating the session token from a cookie or header.
func (sm *SessionManager) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Support both Cookie and Authorization Header
		var token string

		cookie, err := r.Cookie("pok_session")
		if err == nil {
			token = cookie.Value
		}

		if token == "" {
			authHeader := r.Header.Get("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				token = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if token == "" || !sm.ValidateSession(token) {
			http.Error(w, `{"error": "Unauthorized"}`, http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}
