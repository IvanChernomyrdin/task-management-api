package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
)

func (s *Service) commentTeam(ctx context.Context, actorID, taskID int64) (int64, error) {
	var teamID int64

	err := s.DB.QueryRowContext(ctx,
		"SELECT team_id FROM tasks WHERE id=?", taskID,
	).Scan(&teamID)

	if err == sql.ErrNoRows {
		return 0, ErrNotFound
	}

	if err != nil {
		return 0, err
	}

	if _, err = isMember(ctx, s.DB, teamID, actorID); err != nil {
		return 0, err
	}

	return teamID, nil
}

func (s *Service) AddComment(ctx context.Context, actorID, taskID int64, content string) (domain.Comment, error) {
	content = strings.TrimSpace(content)
	if taskID <= 0 || content == "" || len(content) > 5000 {
		return domain.Comment{}, ErrInvalid
	}
	if _, err := s.commentTeam(ctx, actorID, taskID); err != nil {
		return domain.Comment{}, err
	}
	now := time.Now().UTC()
	res, err := s.DB.ExecContext(ctx,
		"INSERT INTO task_comments(task_id,user_id,content,created_at) VALUES(?,?,?,?)",
		taskID,
		actorID,
		content,
		now,
	)
	if err != nil {
		return domain.Comment{}, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return domain.Comment{}, err
	}

	return domain.Comment{
		ID:        id,
		TaskID:    taskID,
		UserID:    actorID,
		Content:   content,
		CreatedAt: now,
	}, nil
}

func (s *Service) Comments(ctx context.Context, actorID, taskID int64) ([]domain.Comment, error) {
	if taskID <= 0 {
		return nil, ErrInvalid
	}
	if _, err := s.commentTeam(ctx, actorID, taskID); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx,
		"SELECT id,task_id,user_id,content,created_at FROM task_comments WHERE task_id=? ORDER BY id",
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Comment, 0)
	for rows.Next() {
		var c domain.Comment
		if err = rows.Scan(
			&c.ID,
			&c.TaskID,
			&c.UserID,
			&c.Content,
			&c.CreatedAt,
		); err != nil {
			return nil, err
		}

		out = append(out, c)
	}

	return out, rows.Err()
}
