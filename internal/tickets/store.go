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

package tickets

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("ticket not found")

type Repository interface {
	Create(req CreateTicketRequest) (Ticket, error)
	Get(id int) (Ticket, bool, error)
	List(status string) ([]Ticket, error)
	Update(id int, req UpdateTicketRequest) (Ticket, error)
	AddComment(id int, req AddCommentRequest) (Ticket, error)
	Stats() (Stats, error)
}

type Store struct {
	mu      sync.RWMutex
	nextID  int
	tickets map[int]*Ticket
}

func NewStore() *Store {
	return &Store{
		nextID:  1,
		tickets: make(map[int]*Ticket),
	}
}

func (s *Store) Create(req CreateTicketRequest) (Ticket, error) {
	customer := strings.TrimSpace(req.Customer)
	email := strings.TrimSpace(req.Email)
	subject := strings.TrimSpace(req.Subject)
	description := strings.TrimSpace(req.Description)
	priority := normalizePriority(req.Priority)

	if customer == "" || email == "" || subject == "" || description == "" {
		return Ticket{}, errors.New("customer, email, subject and description are required")
	}
	if !isValidPriority(priority) {
		return Ticket{}, errors.New("priority must be low, medium, high, or urgent")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	t := &Ticket{
		ID:          s.nextID,
		Customer:    customer,
		Email:       email,
		Subject:     subject,
		Description: description,
		Priority:    priority,
		Status:      StatusOpen,
		Assignees:   make([]string, 0),
		Comments:    make([]Comment, 0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.tickets[t.ID] = t
	s.nextID++
	return copyTicket(t), nil
}

func (s *Store) Get(id int) (Ticket, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tickets[id]
	if !ok {
		return Ticket{}, false, nil
	}
	return copyTicket(t), true, nil
}

func (s *Store) List(status string) ([]Ticket, error) {
	normalizedStatus := strings.TrimSpace(strings.ToLower(status))

	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Ticket, 0, len(s.tickets))
	for _, t := range s.tickets {
		if normalizedStatus != "" && t.Status != normalizedStatus {
			continue
		}
		out = append(out, copyTicket(t))
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Status == out[j].Status {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func (s *Store) Update(id int, req UpdateTicketRequest) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tickets[id]
	if !ok {
		return Ticket{}, ErrNotFound
	}

	if status := strings.TrimSpace(strings.ToLower(req.Status)); status != "" {
		if !isValidStatus(status) {
			return Ticket{}, errors.New("invalid status")
		}
		t.Status = status
	}

	if req.Assignees != nil {
		normalized, err := normalizeAssignees(req.Assignees)
		if err != nil {
			return Ticket{}, err
		}
		t.Assignees = normalized
	}

	if resolution := strings.TrimSpace(req.Resolution); resolution != "" {
		t.Resolution = resolution
		if t.Status == StatusOpen || t.Status == StatusInProgress {
			t.Status = StatusResolved
		}
	}

	t.UpdatedAt = time.Now().UTC()
	return copyTicket(t), nil
}

func (s *Store) AddComment(id int, req AddCommentRequest) (Ticket, error) {
	author := strings.TrimSpace(req.Author)
	message := strings.TrimSpace(req.Message)
	if author == "" || message == "" {
		return Ticket{}, errors.New("author and message are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tickets[id]
	if !ok {
		return Ticket{}, ErrNotFound
	}

	t.Comments = append(t.Comments, Comment{
		Author:    author,
		Message:   message,
		Internal:  req.Internal,
		CreatedAt: time.Now().UTC(),
	})

	if t.Status == StatusOpen {
		t.Status = StatusInProgress
	}
	t.UpdatedAt = time.Now().UTC()

	return copyTicket(t), nil
}

func (s *Store) Stats() (Stats, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := Stats{}
	for _, t := range s.tickets {
		switch t.Status {
		case StatusOpen:
			stats.Open++
		case StatusInProgress:
			stats.InProgress++
		case StatusResolved:
			stats.Resolved++
		case StatusClosed:
			stats.Closed++
		}
		stats.Total++
	}
	return stats, nil
}

func copyTicket(t *Ticket) Ticket {
	out := *t
	out.Comments = append(make([]Comment, 0, len(t.Comments)), t.Comments...)
	out.Assignees = append(make([]string, 0, len(t.Assignees)), t.Assignees...)
	return out
}

func isValidPriority(priority string) bool {
	switch priority {
	case "low", "medium", "high", "urgent":
		return true
	default:
		return false
	}
}

func normalizePriority(priority string) string {
	candidate := strings.TrimSpace(strings.ToLower(priority))
	if candidate == "" {
		return "medium"
	}
	return candidate
}

func isValidStatus(status string) bool {
	switch status {
	case StatusOpen, StatusInProgress, StatusResolved, StatusClosed:
		return true
	default:
		return false
	}
}

func normalizeAssignees(emails []string) ([]string, error) {
	seen := make(map[string]struct{}, len(emails))
	out := make([]string, 0, len(emails))
	for _, raw := range emails {
		email := strings.ToLower(strings.TrimSpace(raw))
		if email == "" {
			continue
		}
		if !isLikelyEmail(email) {
			return nil, errors.New("assignees must contain valid email addresses")
		}
		if _, ok := seen[email]; ok {
			continue
		}
		seen[email] = struct{}{}
		out = append(out, email)
	}
	return out, nil
}

func isLikelyEmail(email string) bool {
	if len(email) < 3 {
		return false
	}
	at := strings.Index(email, "@")
	if at <= 0 || at == len(email)-1 {
		return false
	}
	if strings.Index(email[at+1:], "@") != -1 {
		return false
	}
	return strings.Contains(email[at+1:], ".")
}
