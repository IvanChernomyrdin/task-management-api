package domain

import (
	"encoding/json"
	"time"
)

const (
	RoleOwner        = "owner"
	RoleAdmin        = "admin"
	RoleMember       = "member"
	StatusTodo       = "todo"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
)

type User struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type Team struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CreatedBy int64     `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	Role      string    `json:"role,omitempty"`
}

type Task struct {
	ID          int64      `json:"id"`
	TeamID      int64      `json:"team_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	CreatedBy   int64      `json:"created_by"`
	AssigneeID  *int64     `json:"assignee_id"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	Version     int64      `json:"version"`
}

type History struct {
	ID        int64           `json:"id"`
	TaskID    int64           `json:"task_id"`
	ChangedBy int64           `json:"changed_by"`
	Changes   json.RawMessage `json:"changes"`
	CreatedAt time.Time       `json:"created_at"`
}

type Comment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	UserID    int64     `json:"user_id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type ListFilter struct {
	TeamID     int64  `json:"team_id"`
	Status     string `json:"status,omitempty"`
	AssigneeID *int64 `json:"assignee_id,omitempty"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

type TaskPage struct {
	Items  []Task `json:"items"`
	Total  int64  `json:"total"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

type AssigneeStats struct {
	UserID      int64  `json:"user_id"`
	Name        string `json:"name"`
	ClosedCount int64  `json:"closed_count"`
}

type Stats struct {
	TeamID            int64            `json:"team_id"`
	ByStatus          map[string]int64 `json:"by_status"`
	TopAssignees      []AssigneeStats  `json:"top_assignees_last_30_days"`
	AvgClosureSeconds *float64         `json:"average_closure_seconds"`
	CommentsCount     int64            `json:"comments_count"`
}
