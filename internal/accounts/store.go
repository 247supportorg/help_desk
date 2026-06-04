package accounts

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidEmail      = errors.New("invalid email")
	ErrWeakPassword      = errors.New("password must be at least 8 characters")
	ErrInvalidResetToken = errors.New("invalid or expired reset token")
	ErrEmailExists       = errors.New("email already registered")
	ErrEmailNotFound     = errors.New("email not found")
)

type Repository interface {
	BootstrapFirstUser(email, password string) (bool, error)
	Login(email, password string) (bool, error)
	CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error)
	ResetPassword(token, newPassword string) error
	ListEmails() ([]string, error)
	CreateUser(email, password string) (bool, error)
}

type MemoryStore struct {
	mu     sync.RWMutex
	nextID int
	users  map[string]memoryUser
	byID   map[int]string
	resets map[string]memoryResetToken
}

type memoryUser struct {
	ID           int
	Email        string
	PasswordHash []byte
	CreatedAt    time.Time
}

type memoryResetToken struct {
	UserID    int
	ExpiresAt time.Time
	Used      bool
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nextID: 1,
		users:  make(map[string]memoryUser),
		byID:   make(map[int]string),
		resets: make(map[string]memoryResetToken),
	}
}

func (m *MemoryStore) BootstrapFirstUser(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return false, ErrInvalidEmail
	}
	if len(password) < 8 {
		return false, ErrWeakPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash password failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.users) > 0 {
		return false, nil
	}

	m.users[email] = memoryUser{
		ID:           m.nextID,
		Email:        email,
		PasswordHash: hash,
		CreatedAt:    time.Now().UTC(),
	}
	m.byID[m.nextID] = email
	m.nextID++
	return true, nil
}

func (m *MemoryStore) Login(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return false, ErrInvalidEmail
	}

	m.mu.RLock()
	user, ok := m.users[email]
	m.mu.RUnlock()
	if !ok {
		return false, nil
	}

	if err := bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)); err != nil {
		return false, nil
	}
	return true, nil
}

func (m *MemoryStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return "", false, ErrInvalidEmail
	}

	m.mu.RLock()
	user, ok := m.users[email]
	m.mu.RUnlock()
	if !ok {
		return "", false, nil
	}

	token, err := generateToken()
	if err != nil {
		return "", false, err
	}
	hash := tokenHash(token)

	m.mu.Lock()
	m.resets[hash] = memoryResetToken{
		UserID:    user.ID,
		ExpiresAt: time.Now().UTC().Add(ttl),
		Used:      false,
	}
	m.mu.Unlock()

	return token, true, nil
}

func (m *MemoryStore) ResetPassword(token, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrWeakPassword
	}

	hash := tokenHash(strings.TrimSpace(token))
	now := time.Now().UTC()

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.resets[hash]
	if !ok || entry.Used || now.After(entry.ExpiresAt) {
		return ErrInvalidResetToken
	}

	email, ok := m.byID[entry.UserID]
	if !ok {
		return ErrInvalidResetToken
	}

	user := m.users[email]
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password failed: %w", err)
	}
	user.PasswordHash = passwordHash
	m.users[email] = user

	entry.Used = true
	m.resets[hash] = entry
	return nil
}

func (m *MemoryStore) ListEmails() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.users))
	for email := range m.users {
		out = append(out, email)
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryStore) CreateUser(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return false, ErrInvalidEmail
	}
	if len(password) < 8 {
		return false, ErrWeakPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash password failed: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.users[email]; exists {
		return false, ErrEmailExists
	}

	m.users[email] = memoryUser{
		ID:           m.nextID,
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UTC(),
	}
	m.byID[m.nextID] = email
	m.nextID++
	return true, nil
}

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

	s := &SQLStore{db: db, driver: driver, postgres: driver == "postgres"}
	if err := s.db.Ping(); err != nil {
		_ = s.db.Close()
		return nil, fmt.Errorf("db ping failed: %w", err)
	}
	if err := s.ensureSchema(); err != nil {
		_ = s.db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLStore) Close() error {
	return s.db.Close()
}

func (s *SQLStore) BootstrapFirstUser(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return false, ErrInvalidEmail
	}
	if len(password) < 8 {
		return false, ErrWeakPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash password failed: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	countQuery := `SELECT COUNT(*) FROM users`
	var count int
	if err := tx.QueryRow(countQuery).Scan(&count); err != nil {
		return false, fmt.Errorf("count users failed: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	query := fmt.Sprintf(`INSERT INTO users (email, password_hash, created_at) VALUES (%s, %s, %s)`, s.ph(1), s.ph(2), s.ph(3))
	if _, err := tx.Exec(query, email, string(passwordHash), time.Now().UTC()); err != nil {
		return false, fmt.Errorf("insert admin user failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit tx failed: %w", err)
	}
	return true, nil
}

func (s *SQLStore) Login(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return false, ErrInvalidEmail
	}

	query := fmt.Sprintf(`SELECT password_hash FROM users WHERE email = %s`, s.ph(1))
	var hash string
	if err := s.db.QueryRow(query, email).Scan(&hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query user failed: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return false, nil
	}
	return true, nil
}

func (s *SQLStore) CreatePasswordResetToken(email string, ttl time.Duration) (string, bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return "", false, ErrInvalidEmail
	}

	query := fmt.Sprintf(`SELECT id FROM users WHERE email = %s`, s.ph(1))
	var userID int
	if err := s.db.QueryRow(query, email).Scan(&userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("query user failed: %w", err)
	}

	token, err := generateToken()
	if err != nil {
		return "", false, err
	}

	insert := fmt.Sprintf(`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at, created_at) VALUES (%s, %s, %s, %s)`, s.ph(1), s.ph(2), s.ph(3), s.ph(4))
	if _, err := s.db.Exec(insert, userID, tokenHash(token), time.Now().UTC().Add(ttl), time.Now().UTC()); err != nil {
		return "", false, fmt.Errorf("create reset token failed: %w", err)
	}

	return token, true, nil
}

func (s *SQLStore) ResetPassword(token, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrWeakPassword
	}
	hashedToken := tokenHash(strings.TrimSpace(token))
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password failed: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	selectQuery := fmt.Sprintf(`SELECT id, user_id, expires_at, used_at FROM password_reset_tokens WHERE token_hash = %s`, s.ph(1))
	var resetID int
	var userID int
	var expiresAt time.Time
	var usedAt sql.NullTime
	if err := tx.QueryRow(selectQuery, hashedToken).Scan(&resetID, &userID, &expiresAt, &usedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return fmt.Errorf("query reset token failed: %w", err)
	}
	if usedAt.Valid || time.Now().UTC().After(expiresAt) {
		return ErrInvalidResetToken
	}

	updateUser := fmt.Sprintf(`UPDATE users SET password_hash = %s WHERE id = %s`, s.ph(1), s.ph(2))
	if _, err := tx.Exec(updateUser, string(passwordHash), userID); err != nil {
		return fmt.Errorf("update user password failed: %w", err)
	}

	markUsed := fmt.Sprintf(`UPDATE password_reset_tokens SET used_at = %s WHERE id = %s`, s.ph(1), s.ph(2))
	if _, err := tx.Exec(markUsed, time.Now().UTC(), resetID); err != nil {
		return fmt.Errorf("mark reset token used failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx failed: %w", err)
	}
	return nil
}

func (s *SQLStore) ListEmails() ([]string, error) {
	query := `SELECT email FROM users ORDER BY email ASC`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("list users failed: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("scan user email failed: %w", err)
		}
		out = append(out, email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users failed: %w", err)
	}
	return out, nil
}

func (s *SQLStore) CreateUser(email, password string) (bool, error) {
	email = normalizeEmail(email)
	if !isLikelyEmail(email) {
		return false, ErrInvalidEmail
	}
	if len(password) < 8 {
		return false, ErrWeakPassword
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash password failed: %w", err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin tx failed: %w", err)
	}
	defer tx.Rollback()

	insertQuery := fmt.Sprintf(`INSERT INTO users (email, password_hash, created_at) VALUES (%s, %s, %s)`, s.ph(1), s.ph(2), s.ph(3))
	if _, err := tx.Exec(insertQuery, email, string(passwordHash), time.Now().UTC()); err != nil {
		if isUniqueViolation(err) {
			return false, ErrEmailExists
		}
		return false, fmt.Errorf("insert user failed: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if isUniqueViolation(err) {
			return false, ErrEmailExists
		}
		return false, fmt.Errorf("commit tx failed: %w", err)
	}
	return true, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "duplicate") {
		return true
	}
	if strings.Contains(msg, "unique") {
		return true
	}
	if strings.Contains(msg, "23505") {
		return true
	}
	if strings.Contains(msg, "1062") {
		return true
	}
	return false
}

func (s *SQLStore) ensureSchema() error {
	var statements []string
	if s.postgres {
		statements = []string{
			`CREATE TABLE IF NOT EXISTS users (
				id BIGSERIAL PRIMARY KEY,
				email TEXT NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS password_reset_tokens (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
				token_hash TEXT NOT NULL UNIQUE,
				expires_at TIMESTAMPTZ NOT NULL,
				used_at TIMESTAMPTZ NULL,
				created_at TIMESTAMPTZ NOT NULL
			)`,
			`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user ON password_reset_tokens(user_id)`,
			`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_expires ON password_reset_tokens(expires_at)`,
		}
	} else {
		statements = []string{
			`CREATE TABLE IF NOT EXISTS users (
				id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
				email VARCHAR(320) NOT NULL UNIQUE,
				password_hash TEXT NOT NULL,
				created_at DATETIME(6) NOT NULL
			)`,
			`CREATE TABLE IF NOT EXISTS password_reset_tokens (
				id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
				user_id BIGINT NOT NULL,
				token_hash VARCHAR(128) NOT NULL UNIQUE,
				expires_at DATETIME(6) NOT NULL,
				used_at DATETIME(6) NULL,
				created_at DATETIME(6) NOT NULL,
				INDEX idx_password_reset_tokens_user (user_id),
				INDEX idx_password_reset_tokens_expires (expires_at),
				CONSTRAINT fk_reset_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
			)`,
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

func normalizeEmail(email string) string {
	return strings.TrimSpace(strings.ToLower(email))
}

func isLikelyEmail(email string) bool {
	return strings.Contains(email, "@") && !strings.HasPrefix(email, "@") && !strings.HasSuffix(email, "@")
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token failed: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}
