//go:build integration

package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Run against a real MySQL 8.4 database: TEST_MYSQL_DSN=... go test -tags=integration ./...
func TestStatsQueryIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN is not set")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	ids := make([]int64, 4)

	for i := range ids {
		email := fmt.Sprintf("report-%d-%d@example.test", time.Now().UnixNano(), i)

		r, e := tx.ExecContext(ctx,
			"INSERT INTO users(email,password_hash,name) VALUES(?,?,?)",
			email,
			"fake-hash",
			fmt.Sprintf("User %d", i),
		)

		if e != nil {
			t.Fatal(e)
		}
		ids[i], e = r.LastInsertId()

		if e != nil {
			t.Fatal(e)
		}
	}

	insertTeam := func(name string) int64 {
		t.Helper()

		r, e := tx.ExecContext(ctx,
			"INSERT INTO teams(name,created_by) VALUES(?,?)",
			name, ids[0],
		)

		if e != nil {
			t.Fatal(e)
		}

		id, e := r.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}

		return id
	}

	team := insertTeam("report test")
	otherTeam := insertTeam("another team")
	now := time.Now().UTC()

	insertTask := func(
		teamID, assignee int64,
		status string,
		created, closed time.Time,
	) int64 {
		t.Helper()
		var closedVal any

		if status == "done" {
			closedVal = closed
		}

		r, e := tx.ExecContext(ctx, `
			INSERT INTO tasks (
				team_id,
				title,
				description,
				status,
				created_by,
				assignee_id,
				created_at,
				updated_at,
				closed_at,
				version
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		`,
			teamID,
			"fixture",
			"",
			status,
			ids[0],
			assignee,
			created,
			created,
			closedVal,
		)
		if e != nil {
			t.Fatal(e)
		}

		id, e := r.LastInsertId()
		if e != nil {
			t.Fatal(e)
		}

		return id
	}

	a := insertTask(team, ids[0], "done", now.Add(-time.Hour), now.Add(-30*time.Minute))
	b := insertTask(team, ids[1], "done", now.Add(-2*time.Hour), now.Add(-time.Hour))
	c := insertTask(team, ids[2], "done", now.Add(-3*time.Hour), now.Add(-2*time.Hour))
	_ = insertTask(team, ids[3], "done", now.Add(-4*time.Hour), now.Add(-3*time.Hour))
	_ = insertTask(team, ids[1], "todo", now, now)
	_ = insertTask(otherTeam, ids[0], "done", now.Add(-5*time.Hour), now)

	for _, taskID := range []int64{a, b, c} {
		if _, e := tx.ExecContext(ctx,
			"INSERT INTO task_comments(task_id,user_id,content) VALUES(?,?,?)",
			taskID,
			ids[0],
			"hello",
		); e != nil {
			t.Fatal(e)
		}
	}

	stats, err := StatsQuery(ctx, tx, team)
	if err != nil {
		t.Fatal(err)
	}

	if stats.TeamID != team ||
		stats.ByStatus["done"] != 4 ||
		stats.ByStatus["todo"] != 1 ||
		stats.ByStatus["in_progress"] != 0 {
		t.Fatalf("status leakage or wrong counts: %+v", stats)
	}

	if stats.CommentsCount != 3 {
		t.Fatalf("comments = %d; want 3", stats.CommentsCount)
	}

	if len(stats.TopAssignees) != 3 {
		t.Fatalf("top 3 expected, got %+v", stats.TopAssignees)
	}

	for i, a := range stats.TopAssignees {
		if a.UserID != ids[i] || a.ClosedCount != 1 {
			t.Fatalf("wrong ranking at %d: %+v", i, a)
		}
	}

	if stats.AvgClosureSeconds == nil || *stats.AvgClosureSeconds != 3150 {
		t.Fatalf(
			"average closure = %v; want 3150 seconds",
			stats.AvgClosureSeconds,
		)
	}
}
