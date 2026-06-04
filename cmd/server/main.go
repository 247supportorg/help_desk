// MIT License
//
// Copyright (c) 2026 QB Networks
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"

	"help_desk/internal/accounts"
	"help_desk/internal/tickets"
)

type server struct {
	store      tickets.Repository
	accounts   accounts.Repository
	smtp       smtpConfig
	sessions   map[string]sessionRecord
	sessionsMu sync.RWMutex
}

type writeError struct {
	Error string `json:"error"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

type passwordResetConfirmRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

type signupRequest struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirmPassword"`
}

type adminApprovalRequest struct {
	Email string `json:"email"`
}

type adminDeleteRequest struct {
	Emails []string `json:"emails"`
}

type setupRequest struct {
	AppPort            string `json:"appPort"`
	StoreBackend       string `json:"storeBackend"`
	DBHost             string `json:"dbHost"`
	DBPort             string `json:"dbPort"`
	DBName             string `json:"dbName"`
	DBUser             string `json:"dbUser"`
	DBPassword         string `json:"dbPassword"`
	AdminEmail         string `json:"adminEmail"`
	AdminPassword      string `json:"adminPassword"`
	SMTPHost           string `json:"smtpHost"`
	SMTPPort           string `json:"smtpPort"`
	SMTPUser           string `json:"smtpUser"`
	SMTPPass           string `json:"smtpPass"`
	SMTPFrom           string `json:"smtpFrom"`
	SMTPResetURLBase   string `json:"smtpResetURLBase"`
	WriteEnvFile       bool   `json:"writeEnvFile"`
	EnvPath            string `json:"envPath"`
	EncryptionPassphrase string `json:"encryptionPassphrase"`
	SetupEncPath       string `json:"setupEncPath"`
}

type setupResponse struct {
	Message      string `json:"message"`
	EnvPath      string `json:"envPath"`
	DSN          string `json:"dsn"`
	SetupEncPath string `json:"setupEncPath"`
}

type notifyTicketRequest struct {
	To string `json:"to"`
}

type authResponse struct {
	Authenticated bool   `json:"authenticated"`
	Email         string `json:"email,omitempty"`
}

type sessionRecord struct {
	Email     string
	ExpiresAt time.Time
}

type smtpConfig struct {
	Host         string
	Port         string
	Username     string
	Password     string
	From         string
	ResetURLBase string
}

func (c smtpConfig) Enabled() bool {
	return c.Host != "" && c.Port != "" && c.From != "" && c.ResetURLBase != ""
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "decrypt-setup" {
		runDecryptSetupCLI(os.Args[2:])
		return
	}

	store, closeStore, err := newRepositoryFromEnv()
	if err != nil {
		log.Fatalf("repository setup failed: %v", err)
	}
	defer closeStore()

	accountStore, closeAccounts, err := newAccountsFromEnv()
	if err != nil {
		log.Fatalf("accounts setup failed: %v", err)
	}
	defer closeAccounts()

	if err := bootstrapAdminFromEnv(accountStore); err != nil {
		log.Fatalf("admin bootstrap failed: %v", err)
	}

	s := &server{
		store:    store,
		accounts: accountStore,
		smtp:     loadSMTPConfig(),
		sessions: make(map[string]sessionRecord),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tickets", s.handleTickets)
	mux.HandleFunc("/api/tickets/", s.handleTicketByID)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/admins", s.handleAdmins)
	mux.HandleFunc("/api/admin/pending", s.handleAdminPending)
	mux.HandleFunc("/api/admin/approved", s.handleAdminApproved)
	mux.HandleFunc("/api/admin/rejected", s.handleAdminRejected)
	mux.HandleFunc("/api/admin/approve", s.handleAdminApprove)
	mux.HandleFunc("/api/admin/reject", s.handleAdminReject)
	mux.HandleFunc("/api/admin/delete", s.handleAdminDelete)
	mux.HandleFunc("/api/auth/login", s.handleLogin)
	mux.HandleFunc("/api/auth/signup", s.handleSignup)
	mux.HandleFunc("/api/auth/me", s.handleAuthMe)
	mux.HandleFunc("/api/auth/logout", s.handleLogout)
	mux.HandleFunc("/api/auth/password-reset/request", s.handlePasswordResetRequest)
	mux.HandleFunc("/api/auth/password-reset/confirm", s.handlePasswordResetConfirm)
	mux.HandleFunc("/api/setup/apply", s.handleSetupApply)
	mux.Handle("/", s.staticHandler())

	port := getenv("PORT", "8080")
	addr := ":" + port

	tlsCfg, err := loadTLSConfig()
	if err != nil {
		log.Fatalf("TLS config failed: %v", err)
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(hstsMiddleware(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         tlsCfg,
	}

	if tlsCfg != nil {
		certFile := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
		keyFile := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))
		log.Printf("help desk server listening on %s (HTTPS, AES-GCM)", addr)
		if err := httpServer.ListenAndServeTLS(certFile, keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	} else {
		log.Printf("help desk server listening on %s (HTTP, no TLS - set TLS_CERT_FILE and TLS_KEY_FILE for HTTPS)", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}
}

func newRepositoryFromEnv() (tickets.Repository, func(), error) {
	backend := strings.TrimSpace(strings.ToLower(getenv("STORE_BACKEND", "memory")))
	switch backend {
	case "", "memory", "inmemory":
		return tickets.NewStore(), func() {}, nil
	case "postgres", "postgresql", "mariadb", "mysql":
		dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
		if dsn == "" {
			return nil, nil, errors.New("DB_DSN is required when STORE_BACKEND is postgres or mariadb")
		}

		driver := backend
		if backend == "postgresql" {
			driver = "postgres"
		}
		if backend == "mariadb" {
			driver = "mysql"
		}

		repo, err := tickets.NewSQLStore(driver, dsn)
		if err != nil {
			return nil, nil, enrichDBError(err, backend, "ticket store")
		}

		closer := func() {
			if err := repo.Close(); err != nil {
				log.Printf("db close failed: %v", err)
			}
		}

		log.Printf("store backend: %s", backend)
		return repo, closer, nil
	default:
		return nil, nil, errors.New("unsupported STORE_BACKEND (use memory, postgres, or mariadb)")
	}
}

func newAccountsFromEnv() (accounts.Repository, func(), error) {
	backend := strings.TrimSpace(strings.ToLower(getenv("STORE_BACKEND", "memory")))
	switch backend {
	case "", "memory", "inmemory":
		return accounts.NewMemoryStore(), func() {}, nil
	case "postgres", "postgresql", "mariadb", "mysql":
		dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
		if dsn == "" {
			return nil, nil, errors.New("DB_DSN is required when STORE_BACKEND is postgres or mariadb")
		}

		driver := backend
		if backend == "postgresql" {
			driver = "postgres"
		}
		if backend == "mariadb" {
			driver = "mysql"
		}

		repo, err := accounts.NewSQLStore(driver, dsn)
		if err != nil {
			return nil, nil, enrichDBError(err, backend, "accounts store")
		}

		closer := func() {
			if err := repo.Close(); err != nil {
				log.Printf("accounts db close failed: %v", err)
			}
		}

		return repo, closer, nil
	default:
		return nil, nil, errors.New("unsupported STORE_BACKEND (use memory, postgres, or mariadb)")
	}
}

func bootstrapAdminFromEnv(repo accounts.Repository) error {
	email := strings.TrimSpace(os.Getenv("ADMIN_EMAIL"))
	password := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD"))
	if email == "" || password == "" {
		return nil
	}

	created, err := repo.BootstrapFirstUser(email, password)
	if err != nil {
		return err
	}
	if created {
		log.Printf("admin account created for %s", email)
	}
	return nil
}

func loadSMTPConfig() smtpConfig {
	return smtpConfig{
		Host:         strings.TrimSpace(os.Getenv("SMTP_HOST")),
		Port:         strings.TrimSpace(getenv("SMTP_PORT", "587")),
		Username:     strings.TrimSpace(os.Getenv("SMTP_USER")),
		Password:     strings.TrimSpace(os.Getenv("SMTP_PASS")),
		From:         strings.TrimSpace(os.Getenv("SMTP_FROM")),
		ResetURLBase: strings.TrimSpace(os.Getenv("SMTP_RESET_URL_BASE")),
	}
}

func (s *server) staticHandler() http.Handler {
	wd, err := os.Getwd()
	if err != nil {
		log.Printf("could not read working directory: %v", err)
		return http.NotFoundHandler()
	}

	staticDir := filepath.Join(wd, "web", "static")
	fs := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/admin/login" {
			http.ServeFile(w, r, filepath.Join(staticDir, "admin-login.html"))
			return
		}
		if r.URL.Path == "/admin" {
			if !s.isAuthenticated(r) {
				http.Redirect(w, r, "/admin/login", http.StatusFound)
				return
			}
			http.ServeFile(w, r, filepath.Join(staticDir, "admin-dashboard.html"))
			return
		}
		if r.URL.Path == "/setup" {
			http.ServeFile(w, r, filepath.Join(staticDir, "setup.html"))
			return
		}
		if r.URL.Path == "/reset-password" {
			http.ServeFile(w, r, filepath.Join(staticDir, "reset-password.html"))
			return
		}
		if r.URL.Path == "/" {
			http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
}

func (s *server) handleTickets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		status := r.URL.Query().Get("status")
		items, err := s.store.List(status)
		if err != nil {
			writeStoreError(w, err, "list tickets")
			return
		}
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req tickets.CreateTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
			return
		}

		item, err := s.store.Create(req)
		if err != nil {
			writeStoreError(w, err, "create ticket")
			return
		}
		writeJSON(w, http.StatusCreated, item)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
	}
}

func (s *server) handleTicketByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/tickets/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "missing ticket id"})
		return
	}

	id, err := strconv.Atoi(parts[0])
	if err != nil || id < 1 {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid ticket id"})
		return
	}

	if len(parts) == 1 {
		s.handleSingleTicket(w, r, id)
		return
	}

	if len(parts) == 2 && parts[1] == "comments" {
		s.handleTicketComments(w, r, id)
		return
	}

	if len(parts) == 2 && parts[1] == "notify" {
		s.handleTicketNotify(w, r, id)
		return
	}

	writeJSON(w, http.StatusNotFound, writeError{Error: "endpoint not found"})
}

func (s *server) handleSingleTicket(w http.ResponseWriter, r *http.Request, id int) {
	switch r.Method {
	case http.MethodGet:
		item, ok, err := s.store.Get(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
			log.Printf("get ticket failed: %v", err)
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, writeError{Error: "ticket not found"})
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodPatch:
		var req tickets.UpdateTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
			return
		}

		item, err := s.store.Update(id, req)
		if err != nil {
			writeStoreError(w, err, "update ticket")
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
	}
}

func (s *server) handleTicketComments(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	var req tickets.AddCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}

	item, err := s.store.AddComment(id, req)
	if err != nil {
		writeStoreError(w, err, "add comment")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	stats, err := s.store.Stats()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		log.Printf("stats failed: %v", err)
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

func (s *server) handleAdmins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}

	emails, err := s.accounts.ListEmails()
	if err != nil {
		log.Printf("list admins failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"emails": emails})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}

	ok, err := s.accounts.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, accounts.ErrAccountPending) {
			writeJSON(w, http.StatusForbidden, writeError{Error: err.Error()})
			return
		}
		writeAccountError(w, err, "login")
		return
	}
	if !ok {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "invalid email or password"})
		return
	}

	if err := s.createSession(w, r, req.Email); err != nil {
		log.Printf("create session failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	password := req.Password
	confirm := req.ConfirmPassword

	if email == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "email is required"})
		return
	}
	if len(password) < 8 {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "password must be at least 8 characters"})
		return
	}
	if password != confirm {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "passwords do not match"})
		return
	}

	created, err := s.accounts.CreateUser(email, password)
	if err != nil {
		if errors.Is(err, accounts.ErrEmailExists) {
			writeJSON(w, http.StatusConflict, writeError{Error: err.Error()})
			return
		}
		writeAccountError(w, err, "signup")
		return
	}
	if !created {
		writeJSON(w, http.StatusConflict, writeError{Error: "email already registered"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"email":   email,
		"status":  accounts.UserStatusPending,
		"message": "Account created, pending admin approval",
	})
}

func (s *server) handleAdminPending(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}

	pending, err := s.accounts.ListPendingUsers()
	if err != nil {
		log.Printf("list pending users failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}
	if pending == nil {
		pending = []accounts.UserInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"pending": pending})
}

func (s *server) handleAdminApproved(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}
	users, err := s.accounts.ListApprovedUsers()
	if err != nil {
		log.Printf("list approved users failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}
	if users == nil {
		users = []accounts.UserInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *server) handleAdminRejected(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}
	users, err := s.accounts.ListRejectedUsers()
	if err != nil {
		log.Printf("list rejected users failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}
	if users == nil {
		users = []accounts.UserInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *server) handleAdminApprove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}

	var req adminApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "email is required"})
		return
	}

	if err := s.accounts.ApproveUser(email); err != nil {
		if errors.Is(err, accounts.ErrEmailNotFound) {
			writeJSON(w, http.StatusNotFound, writeError{Error: "user not found"})
			return
		}
		log.Printf("approve user failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email, "status": accounts.UserStatusApproved})
}

func (s *server) handleAdminReject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}

	var req adminApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "email is required"})
		return
	}

	if err := s.accounts.RejectUser(email); err != nil {
		if errors.Is(err, accounts.ErrEmailNotFound) {
			writeJSON(w, http.StatusNotFound, writeError{Error: "user not found"})
			return
		}
		if errors.Is(err, accounts.ErrNotPending) {
			writeJSON(w, http.StatusConflict, writeError{Error: "user is not pending approval"})
			return
		}
		log.Printf("reject user failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "email": email})
}

func (s *server) handleAdminDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	currentEmail, ok := s.sessionEmail(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}

	var req adminDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}
	if len(req.Emails) == 0 {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "emails array is required"})
		return
	}

	normalized := make([]string, 0, len(req.Emails))
	currentNorm := strings.ToLower(strings.TrimSpace(currentEmail))
	for _, raw := range req.Emails {
		e := strings.ToLower(strings.TrimSpace(raw))
		if e == "" {
			continue
		}
		if e == currentNorm {
			continue
		}
		normalized = append(normalized, e)
	}
	if len(normalized) == 0 {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "no deletable accounts in request (cannot delete the currently signed-in admin)"})
		return
	}

	deleted, err := s.accounts.DeleteUsers(normalized)
	if err != nil {
		log.Printf("delete users failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"deleted": deleted,
	})
}

func (s *server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	email, ok := s.sessionEmail(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}

	writeJSON(w, http.StatusOK, authResponse{Authenticated: true, Email: email})
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	if err := s.clearSession(w, r); err != nil {
		log.Printf("logout failed: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.smtp.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, writeError{Error: "smtp is not configured"})
		return
	}

	var req passwordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}

	token, shouldSend, err := s.accounts.CreatePasswordResetToken(req.Email, 30*time.Minute)
	if err != nil {
		writeAccountError(w, err, "create password reset token")
		return
	}

	if shouldSend {
		if err := s.sendPasswordResetEmail(req.Email, token); err != nil {
			log.Printf("send reset email failed: %v", err)
			writeJSON(w, http.StatusInternalServerError, writeError{Error: "failed to send reset email"})
			return
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"message": "if account exists, a reset email has been sent"})
}

func (s *server) handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	var req passwordResetConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}

	if err := s.accounts.ResetPassword(req.Token, req.NewPassword); err != nil {
		writeAccountError(w, err, "reset password")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"message": "password updated"})
}

func (s *server) handleSetupApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
		return
	}

	backend := strings.TrimSpace(strings.ToLower(req.StoreBackend))
	if backend != "postgres" && backend != "mariadb" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "storeBackend must be postgres or mariadb"})
		return
	}

	if strings.TrimSpace(req.DBHost) == "" || strings.TrimSpace(req.DBName) == "" || strings.TrimSpace(req.DBUser) == "" || strings.TrimSpace(req.DBPassword) == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "database host, name, user and password are required"})
		return
	}

	if strings.TrimSpace(req.AdminEmail) == "" || strings.TrimSpace(req.AdminPassword) == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "admin email and password are required"})
		return
	}

	if strings.TrimSpace(req.SMTPHost) == "" || strings.TrimSpace(req.SMTPFrom) == "" || strings.TrimSpace(req.SMTPResetURLBase) == "" {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "smtp host, from and reset URL are required"})
		return
	}

	dsn, driver, err := buildSetupDSN(backend, req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
		return
	}

	ticketStore, err := tickets.NewSQLStore(driver, dsn)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: enrichDBError(err, backend, "setup").Error()})
		return
	}
	_ = ticketStore.Close()

	accountStore, err := accounts.NewSQLStore(driver, dsn)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, writeError{Error: "accounts setup failed: " + err.Error()})
		return
	}
	defer accountStore.Close()

	if _, err := accountStore.BootstrapFirstUser(req.AdminEmail, req.AdminPassword); err != nil {
		writeAccountError(w, err, "bootstrap admin")
		return
	}

	envPath := strings.TrimSpace(req.EnvPath)
	if envPath == "" {
		envPath = "help-desk.env"
	}

	if req.WriteEnvFile {
		envContent := buildEnvFile(req, backend, dsn)
		if err := os.WriteFile(envPath, []byte(envContent), 0o600); err != nil {
			writeJSON(w, http.StatusInternalServerError, writeError{Error: "write env file failed: " + err.Error()})
			return
		}
	}

	setupEncPath := strings.TrimSpace(req.SetupEncPath)
	if setupEncPath == "" {
		setupEncPath = "help-desk.setup.enc"
	}

	encPath := ""
	if passphrase := strings.TrimSpace(req.EncryptionPassphrase); passphrase != "" {
		encrypted, err := encryptSetupSnapshot(req, backend, dsn, passphrase)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, writeError{Error: "encrypt setup snapshot failed: " + err.Error()})
			return
		}
		if err := os.WriteFile(setupEncPath, encrypted, 0o600); err != nil {
			writeJSON(w, http.StatusInternalServerError, writeError{Error: "write encrypted setup file failed: " + err.Error()})
			return
		}
		encPath = setupEncPath
	}

	writeJSON(w, http.StatusOK, setupResponse{
		Message:      "setup validated successfully",
		EnvPath:      envPath,
		DSN:          dsn,
		SetupEncPath: encPath,
	})
}

func buildSetupDSN(backend string, req setupRequest) (string, string, error) {
	host := strings.TrimSpace(req.DBHost)
	port := strings.TrimSpace(req.DBPort)
	name := strings.TrimSpace(req.DBName)
	user := strings.TrimSpace(req.DBUser)
	pass := strings.TrimSpace(req.DBPassword)

	if port == "" {
		if backend == "postgres" {
			port = "5432"
		} else {
			port = "3306"
		}
	}

	switch backend {
	case "postgres":
		dsn := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable", user, pass, host, port, name)
		return dsn, "postgres", nil
	case "mariadb":
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&multiStatements=true", user, pass, host, port, name)
		return dsn, "mysql", nil
	default:
		return "", "", errors.New("unsupported backend")
	}
}

func buildEnvFile(req setupRequest, backend, dsn string) string {
	port := strings.TrimSpace(req.AppPort)
	if port == "" {
		port = "8080"
	}

	smtpPort := strings.TrimSpace(req.SMTPPort)
	if smtpPort == "" {
		smtpPort = "587"
	}

	lines := []string{
		"PORT=" + port,
		"STORE_BACKEND=" + backend,
		"DB_DSN=" + dsn,
		"ADMIN_EMAIL=" + strings.TrimSpace(req.AdminEmail),
		"ADMIN_PASSWORD=" + strings.TrimSpace(req.AdminPassword),
		"SMTP_HOST=" + strings.TrimSpace(req.SMTPHost),
		"SMTP_PORT=" + smtpPort,
		"SMTP_USER=" + strings.TrimSpace(req.SMTPUser),
		"SMTP_PASS=" + strings.TrimSpace(req.SMTPPass),
		"SMTP_FROM=" + strings.TrimSpace(req.SMTPFrom),
		"SMTP_RESET_URL_BASE=" + strings.TrimSpace(req.SMTPResetURLBase),
	}

	return strings.Join(lines, "\n") + "\n"
}

type setupSnapshot struct {
	CreatedAt       string `json:"createdAt"`
	AppPort         string `json:"appPort"`
	StoreBackend    string `json:"storeBackend"`
	DSN             string `json:"dsn"`
	DBHost          string `json:"dbHost"`
	DBPort          string `json:"dbPort"`
	DBName          string `json:"dbName"`
	DBUser          string `json:"dbUser"`
	DBPassword      string `json:"dbPassword"`
	AdminEmail      string `json:"adminEmail"`
	AdminPassword   string `json:"adminPassword"`
	SMTPHost        string `json:"smtpHost"`
	SMTPPort        string `json:"smtpPort"`
	SMTPUser        string `json:"smtpUser"`
	SMTPPass        string `json:"smtpPass"`
	SMTPFrom        string `json:"smtpFrom"`
	SMTPResetURLBase string `json:"smtpResetURLBase"`
}

const (
	setupEncMagic = "HDSK1"
	setupEncKeyLen = 32
	setupEncSaltLen = 16
	setupEncNonceLen = 12
	setupEncScryptN = 1 << 15
	setupEncScryptR = 8
	setupEncScryptP = 1
)

func encryptSetupSnapshot(req setupRequest, backend, dsn, passphrase string) ([]byte, error) {
	if len(passphrase) < 8 {
		return nil, errors.New("encryption passphrase must be at least 8 characters")
	}
	snap := setupSnapshot{
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		AppPort:          strings.TrimSpace(req.AppPort),
		StoreBackend:     backend,
		DSN:              dsn,
		DBHost:           strings.TrimSpace(req.DBHost),
		DBPort:           strings.TrimSpace(req.DBPort),
		DBName:           strings.TrimSpace(req.DBName),
		DBUser:           strings.TrimSpace(req.DBUser),
		DBPassword:       req.DBPassword,
		AdminEmail:       strings.TrimSpace(req.AdminEmail),
		AdminPassword:    req.AdminPassword,
		SMTPHost:         strings.TrimSpace(req.SMTPHost),
		SMTPPort:         strings.TrimSpace(req.SMTPPort),
		SMTPUser:         strings.TrimSpace(req.SMTPUser),
		SMTPPass:         req.SMTPPass,
		SMTPFrom:         strings.TrimSpace(req.SMTPFrom),
		SMTPResetURLBase: strings.TrimSpace(req.SMTPResetURLBase),
	}
	plaintext, err := json.Marshal(snap)
	if err != nil {
		return nil, err
	}

	salt := make([]byte, setupEncSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(passphrase), salt, setupEncScryptN, setupEncScryptR, setupEncScryptP, setupEncKeyLen)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, []byte(setupEncMagic))

	out := make([]byte, 0, len(setupEncMagic)+setupEncSaltLen+len(nonce)+len(ciphertext))
	out = append(out, []byte(setupEncMagic)...)
	out = append(out, salt...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func decryptSetupFile(path, passphrase string) (*setupSnapshot, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < len(setupEncMagic)+setupEncSaltLen+setupEncNonceLen+16 {
		return nil, errors.New("encrypted setup file is truncated or corrupted")
	}
	if string(raw[:len(setupEncMagic)]) != setupEncMagic {
		return nil, errors.New("not a help-desk encrypted setup file")
	}
	off := len(setupEncMagic)
	salt := raw[off : off+setupEncSaltLen]
	off += setupEncSaltLen
	nonce := raw[off : off+setupEncNonceLen]
	off += setupEncNonceLen
	ciphertext := raw[off:]

	key, err := scrypt.Key([]byte(passphrase), salt, setupEncScryptN, setupEncScryptR, setupEncScryptP, setupEncKeyLen)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, []byte(setupEncMagic))
	if err != nil {
		return nil, errors.New("decryption failed: wrong passphrase or corrupted file")
	}
	var snap setupSnapshot
	if err := json.Unmarshal(plaintext, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func runDecryptSetupCLI(args []string) {
	fs := flag.NewFlagSet("decrypt-setup", flag.ExitOnError)
	passphrase := fs.String("passphrase", "", "passphrase to decrypt the setup file (or set HDSK_SETUP_PASSPHRASE)")
	showValues := fs.Bool("show-secrets", true, "print all decrypted values (set --show-secrets=false to mask)")
	out := fs.String("output", "text", "output format: text or env")
	_ = fs.Parse(args)

	if *passphrase == "" {
		*passphrase = os.Getenv("HDSK_SETUP_PASSPHRASE")
	}
	if *passphrase == "" {
		fmt.Fprintln(os.Stderr, "decrypt-setup: --passphrase (or HDSK_SETUP_PASSPHRASE) is required")
		os.Exit(2)
	}
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "decrypt-setup: path to encrypted setup file is required")
		os.Exit(2)
	}
	path := fs.Arg(0)

	snap, err := decryptSetupFile(path, *passphrase)
	if err != nil {
		fmt.Fprintln(os.Stderr, "decrypt-setup:", err)
		os.Exit(1)
	}

	mask := func(v string) string {
		if *showValues || v == "" {
			return v
		}
		return "***"
	}

	if *out == "env" {
		fmt.Printf("PORT=%s\n", snap.AppPort)
		fmt.Printf("STORE_BACKEND=%s\n", snap.StoreBackend)
		fmt.Printf("DB_DSN=%s\n", snap.DSN)
		fmt.Printf("ADMIN_EMAIL=%s\n", snap.AdminEmail)
		fmt.Printf("ADMIN_PASSWORD=%s\n", snap.AdminPassword)
		fmt.Printf("SMTP_HOST=%s\n", snap.SMTPHost)
		fmt.Printf("SMTP_PORT=%s\n", snap.SMTPPort)
		fmt.Printf("SMTP_USER=%s\n", snap.SMTPUser)
		fmt.Printf("SMTP_PASS=%s\n", snap.SMTPPass)
		fmt.Printf("SMTP_FROM=%s\n", snap.SMTPFrom)
		fmt.Printf("SMTP_RESET_URL_BASE=%s\n", snap.SMTPResetURLBase)
		return
	}

	fmt.Printf("CreatedAt          : %s\n", snap.CreatedAt)
	fmt.Printf("AppPort            : %s\n", mask(snap.AppPort))
	fmt.Printf("StoreBackend       : %s\n", snap.StoreBackend)
	fmt.Printf("DBHost:DBPort      : %s:%s\n", snap.DBHost, mask(snap.DBPort))
	fmt.Printf("DBName             : %s\n", snap.DBName)
	fmt.Printf("DBUser             : %s\n", snap.DBUser)
	fmt.Printf("DBPassword         : %s\n", mask(snap.DBPassword))
	fmt.Printf("DSN                : %s\n", mask(snap.DSN))
	fmt.Printf("AdminEmail         : %s\n", snap.AdminEmail)
	fmt.Printf("AdminPassword      : %s\n", mask(snap.AdminPassword))
	fmt.Printf("SMTPHost:SMTPPort  : %s:%s\n", snap.SMTPHost, mask(snap.SMTPPort))
	fmt.Printf("SMTPUser           : %s\n", snap.SMTPUser)
	fmt.Printf("SMTPPass           : %s\n", mask(snap.SMTPPass))
	fmt.Printf("SMTPFrom           : %s\n", snap.SMTPFrom)
	fmt.Printf("SMTPResetURLBase   : %s\n", snap.SMTPResetURLBase)
	_ = sha256.New()
}

func enrichDBError(err error, backend, context string) error {
	message := err.Error()
	hint := ""
	if strings.Contains(message, "connection refused") || strings.Contains(message, "db ping failed") {
		switch backend {
		case "mariadb", "mysql":
			hint = " Start MariaDB with: docker compose up -d mariadb or sudo systemctl start mariadb."
		case "postgres", "postgresql":
			hint = " Start PostgreSQL with: docker compose up -d postgres or sudo systemctl start postgresql."
		}
	}

	if hint == "" {
		return fmt.Errorf("%s database connection failed: %w", context, err)
	}
	return fmt.Errorf("%s database connection failed: %w.%s", context, err, hint)
}

func (s *server) handleTicketNotify(w http.ResponseWriter, r *http.Request, id int) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}
	if !s.isAuthenticated(r) {
		writeJSON(w, http.StatusUnauthorized, writeError{Error: "not authenticated"})
		return
	}
	if !s.smtp.Enabled() {
		writeJSON(w, http.StatusServiceUnavailable, writeError{Error: "smtp is not configured"})
		return
	}

	ticket, ok, err := s.store.Get(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
		log.Printf("get ticket for notify failed: %v", err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, writeError{Error: "ticket not found"})
		return
	}

	var req notifyTicketRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
			return
		}
	}

	recipient := strings.TrimSpace(req.To)
	if recipient == "" {
		recipient = strings.TrimSpace(ticket.Email)
	}
	if recipient == "" {
		recipient = s.smtp.From
	}

	if err := s.sendAssignmentNotificationEmail(recipient, ticket); err != nil {
		log.Printf("send assignment notification failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "failed to send notification email"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "notification email sent",
		"to":      recipient,
	})
}

func (s *server) sendAssignmentNotificationEmail(to string, ticket tickets.Ticket) error {
	subject := fmt.Sprintf("Ticket Assignment Notification - #%d", ticket.ID)
	var body strings.Builder
	body.WriteString("A ticket has been updated in the help desk system and an assignment is pending.\r\n\r\n")
	body.WriteString(fmt.Sprintf("Ticket ID: #%d\r\n", ticket.ID))
	body.WriteString(fmt.Sprintf("Subject: %s\r\n", ticket.Subject))
	body.WriteString(fmt.Sprintf("Status: %s\r\n", ticket.Status))
	body.WriteString(fmt.Sprintf("Priority: %s\r\n", ticket.Priority))
	body.WriteString(fmt.Sprintf("Customer: %s <%s>\r\n", ticket.Customer, ticket.Email))
	if ticket.Description != "" {
		body.WriteString("\r\nDescription:\r\n")
		body.WriteString(ticket.Description)
		body.WriteString("\r\n")
	}
	body.WriteString("\r\nPlease check the help desk and assign an administrator.\r\n")

	message := strings.Join([]string{
		fmt.Sprintf("From: %s", s.smtp.From),
		fmt.Sprintf("To: %s", to),
		fmt.Sprintf("Subject: %s", subject),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		body.String(),
	}, "\r\n")

	addr := s.smtp.Host + ":" + s.smtp.Port
	var auth smtp.Auth
	if s.smtp.Username != "" {
		auth = smtp.PlainAuth("", s.smtp.Username, s.smtp.Password, s.smtp.Host)
	}

	return smtp.SendMail(addr, auth, s.smtp.From, []string{to}, []byte(message))
}

func (s *server) sendPasswordResetEmail(email, token string) error {
	resetURL := strings.TrimRight(s.smtp.ResetURLBase, "/") + "?token=" + url.QueryEscape(token)

	message := strings.Join([]string{
		fmt.Sprintf("From: %s", s.smtp.From),
		fmt.Sprintf("To: %s", email),
		"Subject: FactoryX Help Desk Password Reset",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		"A password reset was requested for your FactoryX Help Desk account.",
		"",
		"Open the link below to reset your password:",
		resetURL,
		"",
		"This link expires in 30 minutes.",
	}, "\r\n")

	addr := s.smtp.Host + ":" + s.smtp.Port
	var auth smtp.Auth
	if s.smtp.Username != "" {
		auth = smtp.PlainAuth("", s.smtp.Username, s.smtp.Password, s.smtp.Host)
	}

	if err := smtp.SendMail(addr, auth, s.smtp.From, []string{email}, []byte(message)); err != nil {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write response failed: %v", err)
	}
}

func writeStoreError(w http.ResponseWriter, err error, op string) {
	if errors.Is(err, tickets.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, writeError{Error: "ticket not found"})
		return
	}

	switch err.Error() {
	case "customer, email, subject and description are required":
		writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
	case "priority must be low, medium, high, or urgent":
		writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
	case "invalid status":
		writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
	case "author and message are required":
		writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
	default:
		log.Printf("%s failed: %v", op, err)
		writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
	}
}

func writeAccountError(w http.ResponseWriter, err error, op string) {
	if errors.Is(err, accounts.ErrInvalidEmail) || errors.Is(err, accounts.ErrWeakPassword) || errors.Is(err, accounts.ErrInvalidResetToken) {
		writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
		return
	}
	if errors.Is(err, accounts.ErrEmailExists) {
		writeJSON(w, http.StatusConflict, writeError{Error: err.Error()})
		return
	}

	log.Printf("%s failed: %v", op, err)
	writeJSON(w, http.StatusInternalServerError, writeError{Error: "internal server error"})
}

func (s *server) createSession(w http.ResponseWriter, r *http.Request, email string) error {
	token, err := generateSessionToken()
	if err != nil {
		return err
	}

	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	s.sessionsMu.Lock()
	s.sessions[token] = sessionRecord{Email: strings.TrimSpace(email), ExpiresAt: expiresAt}
	s.sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     "helpdesk_session",
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *server) clearSession(w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie("helpdesk_session")
	if err == nil && cookie.Value != "" {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "helpdesk_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func (s *server) sessionEmail(r *http.Request) (string, bool) {
	cookie, err := r.Cookie("helpdesk_session")
	if err != nil || cookie.Value == "" {
		return "", false
	}

	s.sessionsMu.RLock()
	record, ok := s.sessions[cookie.Value]
	s.sessionsMu.RUnlock()
	if !ok {
		return "", false
	}
	if time.Now().UTC().After(record.ExpiresAt) {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
		return "", false
	}
	return record.Email, true
}

func (s *server) isAuthenticated(r *http.Request) bool {
	_, ok := s.sessionEmail(r)
	return ok
}

func generateSessionToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(started).Truncate(time.Millisecond))
	})
}

func getenv(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func loadTLSConfig() (*tls.Config, error) {
	certFile := strings.TrimSpace(os.Getenv("TLS_CERT_FILE"))
	keyFile := strings.TrimSpace(os.Getenv("TLS_KEY_FILE"))
	if certFile == "" || keyFile == "" {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS cert/key: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
	}, nil
}

func hstsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}
