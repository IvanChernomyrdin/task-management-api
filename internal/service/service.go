package service

import (
	"context"
	"database/sql"
	"log/slog"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/auth"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	DB     *sql.DB
	Redis  *redis.Client
	Auth   auth.Manager
	Logger *slog.Logger
}

func New(db *sql.DB, redisClient *redis.Client, a auth.Manager, logger *slog.Logger) *Service {
	return &Service{
		DB:     db,
		Redis:  redisClient,
		Auth:   a,
		Logger: logger,
	}
}

func (s *Service) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := s.DB.PingContext(ctx); err != nil {
		return err
	}

	return s.Redis.Ping(ctx).Err()
}

func isMember(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, teamID, userID int64) (string, error) {
	var role string
	err := q.QueryRowContext(ctx,
		"SELECT role FROM team_members WHERE team_id=? AND user_id=?",
		teamID,
		userID,
	).Scan(&role)

	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}

	if err != nil {
		return "", err
	}

	return role, nil
}

func isManager(role string) bool {
	return role == "owner" || role == "admin"
}
