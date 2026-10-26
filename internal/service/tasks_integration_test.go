//go:build integration

package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
	"github.com/redis/go-redis/v9"
)

type taskFixture struct {
	db    *sql.DB
	svc   *Service
	owner int64
	team  int64
	task  domain.Task
	users []int64
	teams []int64
}

func newTaskFixture(t *testing.T, withRedis bool) *taskFixture {
	t.Helper()
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not set")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	f := &taskFixture{db: db}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		t.Fatal(err)
	}

	var redisClient *redis.Client
	if withRedis {
		addr := os.Getenv("TEST_REDIS_ADDR")
		if addr == "" {
			addr = "127.0.0.1:6379"
		}
		redisClient = redis.NewClient(&redis.Options{Addr: addr})
		if err := redisClient.Ping(ctx).Err(); err != nil {
			redisClient.Close()
			db.Close()
			t.Fatalf("Redis integration test requires Redis at %s: %v", addr, err)
		}
	}
	f.svc = &Service{DB: db, Redis: redisClient, Logger: slog.Default()}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		for _, teamID := range f.teams {
			for _, statement := range []string{
				"DELETE c FROM task_comments c JOIN tasks t ON t.id=c.task_id WHERE t.team_id=?",
				"DELETE h FROM task_history h JOIN tasks t ON t.id=h.task_id WHERE t.team_id=?",
				"DELETE FROM tasks WHERE team_id=?",
				"DELETE FROM team_members WHERE team_id=?",
				"DELETE FROM teams WHERE id=?",
			} {
				if _, err := db.ExecContext(cleanupCtx, statement, teamID); err != nil {
					t.Errorf("cleanup team %d: %v", teamID, err)
				}
			}
		}
		for _, userID := range f.users {
			if _, err := db.ExecContext(cleanupCtx, "DELETE FROM users WHERE id=?", userID); err != nil {
				t.Errorf("cleanup user %d: %v", userID, err)
			}
		}
		if redisClient != nil {
			_ = redisClient.Close()
		}
		_ = db.Close()
	})

	f.owner = f.addUser(t, "owner")
	team, err := f.svc.CreateTeam(ctx, f.owner, "Integration Team")
	if err != nil {
		t.Fatal(err)
	}
	f.team = team.ID
	f.teams = append(f.teams, team.ID)
	f.task, err = f.svc.CreateTask(ctx, f.owner, NewTask{TeamID: team.ID, Title: "Concurrent task", Status: domain.StatusTodo})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *taskFixture) addUser(t *testing.T, prefix string) int64 {
	t.Helper()
	email := fmt.Sprintf("audit-%s-%d-%d@example.test", prefix, time.Now().UnixNano(), len(f.users))
	res, err := f.db.Exec("INSERT INTO users(email,password_hash,name) VALUES(?,?,?)", email, "integration-fixture", prefix)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	f.users = append(f.users, id)
	return id
}

func TestConcurrentTaskUpdatesIntegration(t *testing.T) {
	f := newTaskFixture(t, false)
	ctx := context.Background()

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, newTitle := range []string{"update A", "update B"} {
		wg.Add(1)
		go func(title string) {
			defer wg.Done()
			<-start
			_, err := f.svc.UpdateTask(ctx, f.owner, f.task.ID, TaskUpdate{ExpectedVersion: 1, Title: &title})
			results <- err
		}(newTitle)
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent update result: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected 1 success and 1 conflict; got successes=%d conflicts=%d", successes, conflicts)
	}

	var version, revisions, history int64
	if err := f.db.QueryRow("SELECT version FROM tasks WHERE id=?", f.task.ID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("SELECT task_revision FROM teams WHERE id=?", f.team).Scan(&revisions); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("SELECT COUNT(*) FROM task_history WHERE task_id=?", f.task.ID).Scan(&history); err != nil {
		t.Fatal(err)
	}
	if version != 2 || revisions != 2 || history != 2 {
		t.Fatalf("lost update or history mismatch: version=%d revision=%d history=%d (want 2,2,2)", version, revisions, history)
	}
}

func TestTaskListRedisInvalidationIntegration(t *testing.T) {
	f := newTaskFixture(t, true)
	ctx := context.Background()
	filter := domain.ListFilter{TeamID: f.team, Status: domain.StatusTodo, Limit: 20}
	initial, err := f.svc.ListTasks(ctx, f.owner, filter)
	if err != nil {
		t.Fatal(err)
	}
	if initial.Total != 1 || len(initial.Items) != 1 {
		t.Fatalf("initial task list: %+v", initial)
	}

	// A second read should get the same result, potentially from Redis.
	if _, err := f.svc.ListTasks(ctx, f.owner, filter); err != nil {
		t.Fatal(err)
	}
	keys, _, err := f.svc.Redis.Scan(ctx, 0, fmt.Sprintf("tasks:list:team:%d:*", f.team), 100).Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) == 0 {
		t.Fatal("Redis cache key was not written")
	}
	for _, key := range keys {
		ttl, err := f.svc.Redis.TTL(ctx, key).Result()
		if err != nil || ttl <= 0 || ttl > 5*time.Minute {
			t.Fatalf("cache key %q TTL = %v, err=%v", key, ttl, err)
		}
	}

	done := domain.StatusDone
	updated, err := f.svc.UpdateTask(ctx, f.owner, f.task.ID, TaskUpdate{ExpectedVersion: 1, Status: &done})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("version = %d; want 2", updated.Version)
	}
	old, err := f.svc.ListTasks(ctx, f.owner, filter)
	if err != nil {
		t.Fatal(err)
	}
	if old.Total != 0 || len(old.Items) != 0 {
		t.Fatalf("stale todo cache after update: %+v", old)
	}
	filter.Status = domain.StatusDone
	newList, err := f.svc.ListTasks(ctx, f.owner, filter)
	if err != nil {
		t.Fatal(err)
	}
	if newList.Total != 1 || len(newList.Items) != 1 || newList.Items[0].ID != updated.ID || newList.Items[0].Version != 2 {
		t.Fatalf("updated task missing from done list: %+v", newList)
	}
}

func TestCrossTeamIsolationIntegration(t *testing.T) {
	f := newTaskFixture(t, false)
	ctx := context.Background()
	otherID := f.addUser(t, "outsider")
	otherTeam, err := f.svc.CreateTeam(ctx, otherID, "Foreign Team")
	if err != nil {
		t.Fatal(err)
	}
	f.teams = append(f.teams, otherTeam.ID)

	assertInvisible := func(name string, err error) {
		t.Helper()
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: got %v; want ErrNotFound", name, err)
		}
	}
	_, err = f.svc.ListTasks(ctx, otherID, domain.ListFilter{TeamID: f.team, Limit: 20})
	assertInvisible("foreign task list", err)
	_, err = f.svc.TaskHistory(ctx, otherID, f.task.ID)
	assertInvisible("foreign history", err)
	_, err = f.svc.Comments(ctx, otherID, f.task.ID)
	assertInvisible("foreign comments", err)
	_, err = f.svc.AddComment(ctx, otherID, f.task.ID, "not allowed")
	assertInvisible("foreign add comment", err)
	_, err = f.svc.TeamStats(ctx, otherID, f.team)
	assertInvisible("foreign stats", err)
	_, err = f.svc.UpdateTask(ctx, otherID, f.task.ID, TaskUpdate{ExpectedVersion: 1, Status: strPtr(domain.StatusDone)})
	assertInvisible("foreign update", err)
	_, err = f.svc.CreateTask(ctx, otherID, NewTask{TeamID: f.team, Title: "not allowed"})
	assertInvisible("foreign create", err)
	_, err = f.svc.CreateTask(ctx, f.owner, NewTask{TeamID: f.team, Title: "invalid assignee", AssigneeID: &otherID})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross-team assignee: got %v; want ErrInvalid", err)
	}
	if err = f.svc.Invite(ctx, f.owner, f.team, otherID, domain.RoleOwner); !errors.Is(err, ErrInvalid) {
		t.Fatalf("owner role assignment: got %v; want ErrInvalid", err)
	}
	if err = f.svc.Invite(ctx, f.owner, f.team, otherID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ChangeRole(ctx, otherID, f.team, f.owner, domain.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member changing owner's role: got %v; want ErrForbidden", err)
	}
	if err = f.svc.ChangeRole(ctx, f.owner, f.team, otherID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err = f.svc.TeamStats(ctx, otherID, f.team); err != nil {
		t.Fatalf("admin cannot read stats: %v", err)
	}
	if err = f.svc.ChangeRole(ctx, otherID, f.team, f.owner, domain.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin changing owner's role: got %v; want ErrForbidden", err)
	}
}

func strPtr(s string) *string { return &s }