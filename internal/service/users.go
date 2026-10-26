package service

import (
	"context"
	"database/sql"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
)

func duplicate(err error) bool {
	var m *mysql.MySQLError
	return errors.As(err, &m) && m.Number == 1062
}

func (s *Service) Register(ctx context.Context, email, password, name string) (domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	parsed, e := mail.ParseAddress(email)

	if e != nil ||
		parsed.Address != email ||
		len(email) > 254 ||
		len(name) < 1 ||
		len(name) > 120 ||
		len(password) < 8 ||
		len(password) > 72 {
		return domain.User{}, ErrInvalid
	}

	hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return domain.User{}, e
	}

	now := time.Now().UTC()
	result, e := s.DB.ExecContext(ctx,
		"INSERT INTO users(email,password_hash,name,created_at) VALUES(?,?,?,?)",
		email, string(hash), name, now)

	if duplicate(e) {
		return domain.User{}, ErrConflict
	}

	if e != nil {
		return domain.User{}, e
	}

	id, e := result.LastInsertId()
	if e != nil {
		return domain.User{}, e
	}

	return domain.User{
		ID:        id,
		Email:     email,
		Name:      name,
		CreatedAt: now,
	}, nil
}

func (s *Service) Login(ctx context.Context, email, password string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var id int64
	var hash string

	err := s.DB.QueryRowContext(ctx,
		"SELECT id,password_hash FROM users WHERE email=?",
		email,
	).Scan(&id, &hash)

	if err == sql.ErrNoRows {
		return "", ErrUnauthorized
	}

	if err != nil {
		return "", err
	}

	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return "", ErrUnauthorized
	}

	return s.Auth.Sign(id)
}
