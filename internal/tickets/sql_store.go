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
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SQLStore struct {
	db       *sql.DB
	driver   string
	postgres bool
}

func NewSQLStore(driver, dsn string) (*SQLStore, error) {
	driver = strings.TrimSpace(strings.ToLower(driver))
	switch driver {
	case "postgres", "postgresql":
		driver = "postgres"
	case "mariadb", "mysql":
		driver = "mysql"
	default:
		return nil, fmt.Errorf("unsupported db driver %q (use postgres or mariadb)", driver)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open db failed: %w", err)
	}

	store := &SQLStore{
		db:       db,
		driver:   driver,
		postgres: driver == "postgres",
	}

	if err := store.db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db ping failed: %w", err)
	}

	if err := store.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) ensureSchema() error {
	var statements []string
	if s.postgres {
		statements = []string{
			`CREATE TABLE IF NOT EXISTS tickets (
				id BIGSERIAL PRIMARY KEY,
				customer TEXT NOT NULL,
				email TEXT NOT NULL,
				subject TEXT NOT NULL,
				description TEXT NOT NULL,
				priority TEXT NOT NULL,
				status TEXT NOT NULL,
				resolution TEXT NOT NULL DEFAULT '',
				created_at TIMESTAMPTZ NOT NULL,
				updated_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS ticket_assignees (
				ticket_id BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
				email TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL,
				PRIMARY KEY (ticket_id, email)
			)`,
			`CREATE TABLE IF NOT EXISTS comments (
				id BIGSERIAL PRIMARY KEY,
				ticket_id BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
				author TEXT NOT NULL,
				message TEXT NOT NULL,
				internal BOOLEAN NOT NULL DEFAULT FALSE,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets(status)`,
			`CREATE INDEX IF NOT EXISTS idx_comments_ticket_id ON comments(ticket_id)`,
		}
	} else {
		statements = []string{
			`CREATE TABLE IF NOT EXISTS tickets (
				id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
				customer TEXT NOT NULL,
				email TEXT NOT NULL,
				subject TEXT NOT NULL,
				description TEXT NOT NULL,
				priority VARCHAR(16) NOT NULL,
				status VARCHAR(32) NOT NULL,
				resolution TEXT NOT NULL,
				created_at DATETIME(6) NOT NULL,
				updated_at DATETIME(6) NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS ticket_assignees (
				ticket_id BIGINT NOT NULL,
				email TEXT NOT NULL,
				created_at DATETIME(6) NOT NULL,
				PRIMARY KEY (ticket_id, email),
				INDEX idx_ticket_assignees_ticket (ticket_id),
				CONSTRAINT fk_ticket_assignees_ticket FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
			)`,
			`CREATE TABLE IF NOT EXISTS comments (
				id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
				ticket_id BIGINT NOT NULL,
				author TEXT NOT NULL,
				message TEXT NOT NULL,
				internal BOOLEAN NOT NULL DEFAULT FALSE,
				created_at DATETIME(6) NOT NULL,
				INDEX idx_comments_ticket_id (ticket_id),
				CONSTRAINT fk_comments_ticket FOREIGN KEY (ticket_id) REFERENCES tickets(id) ON DELETE CASCADE
			)`,
			`CREATE INDEX idx_tickets_status ON tickets(status)`,
		}
	}

	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			// MariaDB doesn't support IF NOT EXISTS for CREATE INDEX in all versions.
			if s.driver == "mysql" && strings.HasPrefix(strings.ToUpper(strings.TrimSpace(stmt)), "CREATE INDEX") {
				if strings.Contains(strings.ToLower(err.Error()), "duplicate key name") {
					continue
				}
			}
			return fmt.Errorf("schema migration failed: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) ph(position int) string {
	if s.postgres {
		return fmt.Sprintf("$%d", position)
	}
	return "?"
}

func (s *SQLStore) Create(req CreateTicketRequest) (Ticket, error) {
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

	now := time.Now().UTC()
	status := StatusOpen
	resolution := ""

	if s.postgres {
		query := `INSERT INTO tickets (customer, email, subject, description, priority, status, resolution, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING id`
		var id int
		if err := s.db.QueryRow(query, customer, email, subject, description, priority, status, resolution, now, now).Scan(&id); err != nil {
			return Ticket{}, fmt.Errorf("create ticket failed: %w", err)
		}
		return Ticket{
			ID:          id,
			Customer:    customer,
			Email:       email,
			Subject:     subject,
			Description: description,
			Priority:    priority,
			Status:      status,
			Assignees:   make([]string, 0),
			Resolution:  resolution,
			Comments:    make([]Comment, 0),
			CreatedAt:   now,
			UpdatedAt:   now,
		}, nil
	}

	query := `INSERT INTO tickets (customer, email, subject, description, priority, status, resolution, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	result, err := s.db.Exec(query, customer, email, subject, description, priority, status, resolution, now, now)
	if err != nil {
		return Ticket{}, fmt.Errorf("create ticket failed: %w", err)
	}

	insertID, err := result.LastInsertId()
	if err != nil {
		return Ticket{}, fmt.Errorf("fetch inserted ticket id failed: %w", err)
	}

	return Ticket{
		ID:          int(insertID),
		Customer:    customer,
		Email:       email,
		Subject:     subject,
		Description: description,
		Priority:    priority,
		Status:      status,
		Assignees:   make([]string, 0),
		Resolution:  resolution,
		Comments:    make([]Comment, 0),
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *SQLStore) Get(id int) (Ticket, bool, error) {
	query := fmt.Sprintf(`SELECT id, customer, email, subject, description, priority, status, resolution, created_at, updated_at
		FROM tickets WHERE id = %s`, s.ph(1))

	var item Ticket
	err := s.db.QueryRow(query, id).Scan(
		&item.ID,
		&item.Customer,
		&item.Email,
		&item.Subject,
		&item.Description,
		&item.Priority,
		&item.Status,
		&item.Resolution,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Ticket{}, false, nil
		}
		return Ticket{}, false, fmt.Errorf("get ticket failed: %w", err)
	}

	assignees, err := s.assigneesByTicketIDs([]int{id})
	if err != nil {
		return Ticket{}, false, err
	}
	item.Assignees = assignees[id]
	if item.Assignees == nil {
		item.Assignees = make([]string, 0)
	}

	comments, err := s.commentsByTicketIDs([]int{id})
	if err != nil {
		return Ticket{}, false, err
	}
	item.Comments = comments[id]
	if item.Comments == nil {
		item.Comments = make([]Comment, 0)
	}

	return item, true, nil
}

func (s *SQLStore) List(status string) ([]Ticket, error) {
	normalizedStatus := strings.TrimSpace(strings.ToLower(status))

	base := `SELECT id, customer, email, subject, description, priority, status, resolution, created_at, updated_at FROM tickets`
	args := make([]any, 0, 1)
	if normalizedStatus != "" {
		base += " WHERE status = " + s.ph(1)
		args = append(args, normalizedStatus)
	}
	base += " ORDER BY id DESC"

	rows, err := s.db.Query(base, args...)
	if err != nil {
		return nil, fmt.Errorf("list tickets failed: %w", err)
	}
	defer rows.Close()

	out := make([]Ticket, 0)
	ids := make([]int, 0)
	for rows.Next() {
		var item Ticket
		if err := rows.Scan(
			&item.ID,
			&item.Customer,
			&item.Email,
			&item.Subject,
			&item.Description,
			&item.Priority,
			&item.Status,
			&item.Resolution,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ticket failed: %w", err)
		}
		out = append(out, item)
		ids = append(ids, item.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tickets failed: %w", err)
	}

	if len(out) == 0 {
		return out, nil
	}

	assigneesByTicket, err := s.assigneesByTicketIDs(ids)
	if err != nil {
		return nil, err
	}

	commentsByTicket, err := s.commentsByTicketIDs(ids)
	if err != nil {
		return nil, err
	}

	for i := range out {
		out[i].Assignees = assigneesByTicket[out[i].ID]
		if out[i].Assignees == nil {
			out[i].Assignees = make([]string, 0)
		}
		out[i].Comments = commentsByTicket[out[i].ID]
		if out[i].Comments == nil {
			out[i].Comments = make([]Comment, 0)
		}
	}

	return out, nil
}

func (s *SQLStore) Update(id int, req UpdateTicketRequest) (Ticket, error) {
	status := strings.TrimSpace(strings.ToLower(req.Status))
	if status != "" && !isValidStatus(status) {
		return Ticket{}, errors.New("invalid status")
	}

	resolution := strings.TrimSpace(req.Resolution)

	var normalizedAssignees []string
	if req.Assignees != nil {
		var err error
		normalizedAssignees, err = normalizeAssignees(req.Assignees)
		if err != nil {
			return Ticket{}, err
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Ticket{}, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	selectQuery := fmt.Sprintf(`SELECT status, resolution FROM tickets WHERE id = %s`, s.ph(1))
	var currentStatus, currentResolution string
	if err := tx.QueryRow(selectQuery, id).Scan(&currentStatus, &currentResolution); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Ticket{}, ErrNotFound
		}
		return Ticket{}, fmt.Errorf("read ticket before update failed: %w", err)
	}

	if status == "" {
		status = currentStatus
	}
	if resolution == "" {
		resolution = currentResolution
	} else if status == StatusOpen || status == StatusInProgress {
		status = StatusResolved
	}

	now := time.Now().UTC()
	updateQuery := fmt.Sprintf(`UPDATE tickets
		SET status = %s, resolution = %s, updated_at = %s
		WHERE id = %s`, s.ph(1), s.ph(2), s.ph(3), s.ph(4))
	result, err := tx.Exec(updateQuery, status, resolution, now, id)
	if err != nil {
		return Ticket{}, fmt.Errorf("update ticket failed: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return Ticket{}, fmt.Errorf("check update result failed: %w", err)
	}
	if affected == 0 {
		return Ticket{}, ErrNotFound
	}

	if req.Assignees != nil {
		deleteQuery := fmt.Sprintf(`DELETE FROM ticket_assignees WHERE ticket_id = %s`, s.ph(1))
		if _, err := tx.Exec(deleteQuery, id); err != nil {
			return Ticket{}, fmt.Errorf("clear assignees failed: %w", err)
		}
		for _, email := range normalizedAssignees {
			insertQuery := fmt.Sprintf(`INSERT INTO ticket_assignees (ticket_id, email, created_at) VALUES (%s, %s, %s)`, s.ph(1), s.ph(2), s.ph(3))
			if _, err := tx.Exec(insertQuery, id, email, now); err != nil {
				return Ticket{}, fmt.Errorf("insert assignee failed: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit tx failed: %w", err)
	}

	item, ok, err := s.Get(id)
	if err != nil {
		return Ticket{}, err
	}
	if !ok {
		return Ticket{}, ErrNotFound
	}
	return item, nil
}

func (s *SQLStore) AddComment(id int, req AddCommentRequest) (Ticket, error) {
	author := strings.TrimSpace(req.Author)
	message := strings.TrimSpace(req.Message)
	if author == "" || message == "" {
		return Ticket{}, errors.New("author and message are required")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return Ticket{}, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	statusQuery := fmt.Sprintf(`SELECT status FROM tickets WHERE id = %s`, s.ph(1))
	var status string
	if err := tx.QueryRow(statusQuery, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Ticket{}, ErrNotFound
		}
		return Ticket{}, fmt.Errorf("read ticket before comment failed: %w", err)
	}

	now := time.Now().UTC()
	insertQuery := fmt.Sprintf(`INSERT INTO comments (ticket_id, author, message, internal, created_at)
		VALUES (%s, %s, %s, %s, %s)`, s.ph(1), s.ph(2), s.ph(3), s.ph(4), s.ph(5))
	if _, err := tx.Exec(insertQuery, id, author, message, req.Internal, now); err != nil {
		return Ticket{}, fmt.Errorf("add comment failed: %w", err)
	}

	nextStatus := status
	if status == StatusOpen {
		nextStatus = StatusInProgress
	}

	updateQuery := fmt.Sprintf(`UPDATE tickets SET status = %s, updated_at = %s WHERE id = %s`, s.ph(1), s.ph(2), s.ph(3))
	if _, err := tx.Exec(updateQuery, nextStatus, now, id); err != nil {
		return Ticket{}, fmt.Errorf("update ticket after comment failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Ticket{}, fmt.Errorf("commit tx failed: %w", err)
	}

	item, ok, err := s.Get(id)
	if err != nil {
		return Ticket{}, err
	}
	if !ok {
		return Ticket{}, ErrNotFound
	}
	return item, nil
}

func (s *SQLStore) Stats() (Stats, error) {
	query := `SELECT
		COUNT(*) AS total,
		COALESCE(SUM(CASE WHEN status = 'open' THEN 1 ELSE 0 END), 0) AS open_count,
		COALESCE(SUM(CASE WHEN status = 'in_progress' THEN 1 ELSE 0 END), 0) AS in_progress_count,
		COALESCE(SUM(CASE WHEN status = 'resolved' THEN 1 ELSE 0 END), 0) AS resolved_count,
		COALESCE(SUM(CASE WHEN status = 'closed' THEN 1 ELSE 0 END), 0) AS closed_count
	FROM tickets`

	var stats Stats
	if err := s.db.QueryRow(query).Scan(
		&stats.Total,
		&stats.Open,
		&stats.InProgress,
		&stats.Resolved,
		&stats.Closed,
	); err != nil {
		return Stats{}, fmt.Errorf("stats query failed: %w", err)
	}
	return stats, nil
}

func (s *SQLStore) assigneesByTicketIDs(ids []int) (map[int][]string, error) {
	out := make(map[int][]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	uniq := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		uniq[id] = struct{}{}
	}
	flat := make([]int, 0, len(uniq))
	for id := range uniq {
		flat = append(flat, id)
	}
	sort.Ints(flat)

	placeholders := make([]string, 0, len(flat))
	args := make([]any, 0, len(flat))
	for i, id := range flat {
		placeholders = append(placeholders, s.ph(i+1))
		args = append(args, id)
	}

	query := fmt.Sprintf(`SELECT ticket_id, email
		FROM ticket_assignees
		WHERE ticket_id IN (%s)
		ORDER BY created_at ASC, email ASC`, strings.Join(placeholders, ", "))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list assignees failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ticketID int
		var email string
		if err := rows.Scan(&ticketID, &email); err != nil {
			return nil, fmt.Errorf("scan assignee failed: %w", err)
		}
		out[ticketID] = append(out[ticketID], email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list assignees failed: %w", err)
	}

	return out, nil
}

func (s *SQLStore) commentsByTicketIDs(ids []int) (map[int][]Comment, error) {
	out := make(map[int][]Comment, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	uniq := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		uniq[id] = struct{}{}
	}
	flat := make([]int, 0, len(uniq))
	for id := range uniq {
		flat = append(flat, id)
	}
	sort.Ints(flat)

	placeholders := make([]string, 0, len(flat))
	args := make([]any, 0, len(flat))
	for i, id := range flat {
		placeholders = append(placeholders, s.ph(i+1))
		args = append(args, id)
	}

	query := fmt.Sprintf(`SELECT ticket_id, author, message, internal, created_at
		FROM comments
		WHERE ticket_id IN (%s)
		ORDER BY created_at ASC`, strings.Join(placeholders, ", "))

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list comments failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ticketID int
		var comment Comment
		if err := rows.Scan(&ticketID, &comment.Author, &comment.Message, &comment.Internal, &comment.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan comment failed: %w", err)
		}
		out[ticketID] = append(out[ticketID], comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list comments failed: %w", err)
	}

	return out, nil
}
