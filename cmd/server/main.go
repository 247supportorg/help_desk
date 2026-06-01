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
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"help_desk/internal/tickets"
)

type server struct {
	store *tickets.Store
}

type writeError struct {
	Error string `json:"error"`
}

func main() {
	store := tickets.NewStore()
	s := &server{store: store}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/tickets", s.handleTickets)
	mux.HandleFunc("/api/tickets/", s.handleTicketByID)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.Handle("/", s.staticHandler())

	port := getenv("PORT", "8080")
	addr := ":" + port

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("help desk server listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server failed: %v", err)
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
		items := s.store.List(status)
		writeJSON(w, http.StatusOK, items)
	case http.MethodPost:
		var req tickets.CreateTicketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, writeError{Error: "invalid JSON payload"})
			return
		}

		item, err := s.store.Create(req)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, writeError{Error: err.Error()})
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

	writeJSON(w, http.StatusNotFound, writeError{Error: "endpoint not found"})
}

func (s *server) handleSingleTicket(w http.ResponseWriter, r *http.Request, id int) {
	switch r.Method {
	case http.MethodGet:
		item, ok := s.store.Get(id)
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
			status := http.StatusBadRequest
			if errors.Is(err, tickets.ErrNotFound) {
				status = http.StatusNotFound
			}
			writeJSON(w, status, writeError{Error: err.Error()})
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
		status := http.StatusBadRequest
		if errors.Is(err, tickets.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, writeError{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, writeError{Error: "method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, s.store.Stats())
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("write response failed: %v", err)
	}
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
