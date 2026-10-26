package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
	"github.com/redis/go-redis/v9"
)

type NewTask struct {
	TeamID      int64  `json:"team_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	AssigneeID  *int64 `json:"assignee_id"`
}

type TaskUpdate struct {
	ExpectedVersion int64
	Title           *string
	Description     *string
	Status          *string
	AssigneeSet     bool
	AssigneeID      *int64
}

func validStatus(s string) bool {
	return s == domain.StatusTodo ||
		s == domain.StatusInProgress ||
		s == domain.StatusDone
}

func scanTask(s interface{ Scan(...any) error }) (t domain.Task, err error) {
	var assignee sql.NullInt64
	var closed sql.NullTime

	err = s.Scan(
		&t.ID,
		&t.TeamID,
		&t.Title,
		&t.Description,
		&t.Status,
		&t.CreatedBy,
		&assignee,
		&t.CreatedAt,
		&t.UpdatedAt,
		&closed,
		&t.Version,
	)
	if err != nil {
		return t, err
	}

	if assignee.Valid {
		t.AssigneeID = &assignee.Int64
	}

	if closed.Valid {
		t.ClosedAt = &closed.Time
	}

	return t, nil
}

func taskCols() string {
	return "id, team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version"
}

func checkAssignee(ctx context.Context, tx *sql.Tx, teamID int64, assigneeID *int64) error {
	if assigneeID == nil {
		return nil
	}

	if *assigneeID <= 0 {
		return ErrInvalid
	}
	_, err := isMember(ctx, tx, teamID, *assigneeID)
	if err == ErrNotFound {
		return ErrInvalid
	}

	return err
}

func ptrValue(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func (s *Service) CreateTask(ctx context.Context, actorID int64, in NewTask) (domain.Task, error) {
	in.Title = strings.TrimSpace(in.Title)

	if in.TeamID <= 0 ||
		in.Title == "" ||
		len(in.Title) > 200 ||
		len(in.Description) > 10000 {
		return domain.Task{}, ErrInvalid
	}

	if in.Status == "" {
		in.Status = domain.StatusTodo
	}

	if !validStatus(in.Status) {
		return domain.Task{}, ErrInvalid
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()

	if _, err = isMember(ctx, tx, in.TeamID, actorID); err != nil {
		return domain.Task{}, err
	}

	if err = checkAssignee(ctx, tx, in.TeamID, in.AssigneeID); err != nil {
		return domain.Task{}, err
	}

	now := time.Now().UTC()
	var closed *time.Time

	if in.Status == domain.StatusDone {
		closed = &now
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO tasks(team_id, title, description, status, created_by, assignee_id, created_at, updated_at, closed_at, version)
		 VALUES(?,?,?,?,?,?,?,?,?,1)`,
		in.TeamID, in.Title, in.Description, in.Status, actorID, ptrValue(in.AssigneeID), now, now, closed,
	)
	if err != nil {
		return domain.Task{}, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return domain.Task{}, err
	}

	task := domain.Task{
		ID:          id,
		TeamID:      in.TeamID,
		Title:       in.Title,
		Description: in.Description,
		Status:      in.Status,
		CreatedBy:   actorID,
		AssigneeID:  in.AssigneeID,
		CreatedAt:   now,
		UpdatedAt:   now,
		ClosedAt:    closed,
		Version:     1,
	}

	changes := map[string]any{
		"created": map[string]any{
			"old": nil,
			"new": task,
		},
	}
	if err = writeHistory(ctx, tx, id, actorID, changes, now); err != nil {
		return domain.Task{}, err
	}

	if _, err = tx.ExecContext(ctx,
		"UPDATE teams SET task_revision=task_revision+1 WHERE id=?", in.TeamID,
	); err != nil {
		return domain.Task{}, err
	}

	if err = tx.Commit(); err != nil {
		return domain.Task{}, err
	}

	return task, nil
}

func writeHistory(ctx context.Context, tx *sql.Tx, taskID, actorID int64, changes any, now time.Time) error {
	payload, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO task_history(task_id,changed_by,changes,created_at) VALUES(?,?,?,?)",
		taskID, actorID, payload, now,
	)

	return err
}

func (s *Service) UpdateTask(ctx context.Context, actorID, taskID int64, in TaskUpdate) (domain.Task, error) {
	if taskID <= 0 || in.ExpectedVersion <= 0 {
		return domain.Task{}, ErrInvalid
	}

	if in.Title == nil &&
		in.Description == nil &&
		in.Status == nil &&
		!in.AssigneeSet {
		return domain.Task{}, ErrInvalid
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return domain.Task{}, err
	}
	defer tx.Rollback()

	t, err := scanTask(tx.QueryRowContext(ctx,
		"SELECT "+taskCols()+" FROM tasks WHERE id=? FOR UPDATE", taskID))
	if err == sql.ErrNoRows {
		return domain.Task{}, ErrNotFound
	}

	if err != nil {
		return domain.Task{}, err
	}

	role, err := isMember(ctx, tx, t.TeamID, actorID)
	if err != nil {
		return domain.Task{}, err
	}

	if t.Version != in.ExpectedVersion {
		return domain.Task{}, ErrConflict
	}

	fields := make([]string, 0, 4)
	if in.Title != nil {
		fields = append(fields, "title")
	}

	if in.Description != nil {
		fields = append(fields, "description")
	}

	if in.Status != nil {
		fields = append(fields, "status")
	}

	if in.AssigneeSet {
		fields = append(fields, "assignee_id")
	}

	if !CanEdit(role, actorID, t.CreatedBy, t.AssigneeID, fields) {
		return domain.Task{}, ErrForbidden
	}

	changes := map[string]any{}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" || len(title) > 200 {
			return domain.Task{}, ErrInvalid
		}
		if title != t.Title {
			changes["title"] = map[string]any{
				"old": t.Title,
				"new": title,
			}
			t.Title = title
		}
	}

	if in.Description != nil {
		if len(*in.Description) > 10000 {
			return domain.Task{}, ErrInvalid
		}
		if *in.Description != t.Description {
			changes["description"] = map[string]any{
				"old": t.Description,
				"new": *in.Description,
			}
			t.Description = *in.Description

		}
	}

	if in.Status != nil {
		if !validStatus(*in.Status) {
			return domain.Task{}, ErrInvalid
		}
		if *in.Status != t.Status {
			changes["status"] = map[string]any{
				"old": t.Status,
				"new": *in.Status,
			}
			t.Status = *in.Status

			oldClosed := t.ClosedAt
			if t.Status == domain.StatusDone {
				now := time.Now().UTC()
				t.ClosedAt = &now
			} else {
				t.ClosedAt = nil
			}
			changes["closed_at"] = map[string]any{
				"old": oldClosed,
				"new": t.ClosedAt,
			}
		}
	}
	if in.AssigneeSet {
		if err = checkAssignee(ctx, tx, t.TeamID, in.AssigneeID); err != nil {
			return domain.Task{}, err
		}
		oldID := ptrValue(t.AssigneeID)
		newID := ptrValue(in.AssigneeID)
		if oldID != newID {
			changes["assignee_id"] = map[string]any{
				"old": oldID,
				"new": newID,
			}
			t.AssigneeID = in.AssigneeID

		}
	}
	if len(changes) == 0 {
		return t, nil
	}

	now := time.Now().UTC()
	t.UpdatedAt = now
	t.Version++

	result, err := tx.ExecContext(ctx,
		`UPDATE tasks SET title=?,description=?,status=?,assignee_id=?,updated_at=?,closed_at=?,version=? WHERE id=? AND version=?`,
		t.Title, t.Description, t.Status, ptrValue(t.AssigneeID), now, t.ClosedAt, t.Version, t.ID, in.ExpectedVersion,
	)
	if err != nil {
		return domain.Task{}, err
	}

	n, err := result.RowsAffected()
	if err != nil {
		return domain.Task{}, err
	}

	if n != 1 {
		return domain.Task{}, ErrConflict
	}

	if err = writeHistory(ctx, tx, t.ID, actorID, changes, now); err != nil {
		return domain.Task{}, err
	}

	if _, err = tx.ExecContext(ctx,
		"UPDATE teams SET task_revision=task_revision+1 WHERE id=?", t.TeamID,
	); err != nil {
		return domain.Task{}, err
	}

	if err = tx.Commit(); err != nil {
		return domain.Task{}, err
	}

	return t, nil
}

func (s *Service) ListTasks(ctx context.Context, actorID int64, f domain.ListFilter) (domain.TaskPage, error) {
	result := domain.TaskPage{
		Items:  []domain.Task{},
		Limit:  f.Limit,
		Offset: f.Offset,
	}
	if f.TeamID <= 0 ||
		f.Limit < 1 ||
		f.Limit > 100 ||
		f.Offset < 0 ||
		f.Offset > 1000000 ||
		(f.Status != "" && !validStatus(f.Status)) ||
		(f.AssigneeID != nil && *f.AssigneeID <= 0) {
		return result, ErrInvalid
	}

	var revision int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT t.task_revision FROM teams t JOIN team_members m ON m.team_id=t.id WHERE t.id=? AND m.user_id=?`,
		f.TeamID, actorID,
	).Scan(&revision)

	if err == sql.ErrNoRows {
		return result, ErrNotFound
	}

	if err != nil {
		return result, err
	}

	keyBytes, _ := json.Marshal([]any{
		f.TeamID, revision,
		f.Status,
		f.AssigneeID,
		f.Limit,
		f.Offset,
	})

	sum := sha256.Sum256(keyBytes)

	cacheKey := fmt.Sprintf("tasks:list:team:%d:rev:%d:%s", f.TeamID, revision, hex.EncodeToString(sum[:]))

	if s.Redis != nil {
		cached, redisErr := s.Redis.Get(ctx, cacheKey).Bytes()
		if redisErr == nil && json.Unmarshal(cached, &result) == nil {
			return result, nil
		}
		if redisErr != nil && !errors.Is(redisErr, redis.Nil) {
			s.Logger.Warn("redis read failed; using mysql", "error", redisErr)
		}
	}

	conditions := "team_id=?"
	args := []any{f.TeamID}

	if f.Status != "" {
		conditions += " AND status=?"
		args = append(args, f.Status)
	}

	if f.AssigneeID != nil {
		conditions += " AND assignee_id=?"
		args = append(args, *f.AssigneeID)
	}

	err = s.DB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM tasks WHERE "+conditions, args...).Scan(&result.Total)
	if err != nil {
		return result, err
	}

	rows, err := s.DB.QueryContext(ctx,
		"SELECT "+taskCols()+" FROM tasks WHERE "+conditions+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, f.Limit, f.Offset)...)
	if err != nil {
		return result, err
	}

	defer rows.Close()
	for rows.Next() {
		t, e := scanTask(rows)
		if e != nil {
			return result, e
		}
		result.Items = append(result.Items, t)
	}

	if err = rows.Err(); err != nil {
		return result, err
	}

	if s.Redis != nil {
		payload, e := json.Marshal(result)
		if e == nil {
			if e = s.Redis.Set(ctx, cacheKey, payload, 5*time.Minute).Err(); e != nil {
				s.Logger.Warn("redis write failed", "error", e)
			}
		}
	}

	return result, nil
}

func (s *Service) TaskHistory(ctx context.Context, actorID, taskID int64) ([]domain.History, error) {
	if taskID <= 0 {
		return nil, ErrInvalid
	}
	var teamID int64
	err := s.DB.QueryRowContext(ctx, "SELECT team_id FROM tasks WHERE id=?", taskID).Scan(&teamID)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, err
	}

	if _, err = isMember(ctx, s.DB, teamID, actorID); err != nil {
		return nil, err
	}

	rows, err := s.DB.QueryContext(ctx,
		"SELECT id,task_id,changed_by,changes,created_at FROM task_history WHERE task_id=? ORDER BY id",
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.History, 0)

	for rows.Next() {
		var h domain.History
		if err = rows.Scan(
			&h.ID,
			&h.TaskID,
			&h.ChangedBy,
			&h.Changes,
			&h.CreatedAt,
		); err != nil {
			return nil, err
		}

		out = append(out, h)
	}

	return out, rows.Err()
}
