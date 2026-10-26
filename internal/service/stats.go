package service

import (
	"context"
	"database/sql"
	"sort"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
)

// StatsSQL uses one CTE with preaggregated comments so that JOINs cannot multiply task counts.
// The result consists of rows with a metric discriminator, all from one team.
const StatsSQL = `
WITH
    team_tasks AS (
        SELECT
            id,
            status,
            assignee_id,
            created_at,
            closed_at
        FROM
            tasks
        WHERE
            team_id = ?
    ),
    statuses AS (
        SELECT
            status,
            COUNT(*) AS amount
        FROM
            team_tasks
        GROUP BY
            status
    ),
    assignees AS (
        SELECT
            t.assignee_id AS user_id,
            u.name,
            COUNT(*) AS amount
        FROM
            team_tasks t
            JOIN users u ON u.id = t.assignee_id
        WHERE
            t.status = 'done'
            AND t.closed_at >= UTC_TIMESTAMP (6) - INTERVAL 30 DAY
        GROUP BY
            t.assignee_id,
            u.name
    ),
    ranked_assignees AS (
        SELECT
            user_id,
            name,
            amount,
            ROW_NUMBER() OVER (
                ORDER BY
                    amount DESC,
                    user_id ASC
            ) AS rank_no
        FROM
            assignees
    ),
    closure AS (
        SELECT
            AVG(
                TIMESTAMPDIFF (MICROSECOND, created_at, closed_at) / 1000000.0
            ) AS average_seconds
        FROM
            team_tasks
        WHERE
            status = 'done'
            AND closed_at IS NOT NULL
    ),
    comments AS (
        SELECT
            COUNT(c.id) AS amount
        FROM
            team_tasks t
            JOIN task_comments c ON c.task_id = t.id
    )
SELECT
    'status' AS metric,
    status AS label,
    CAST(NULL AS SIGNED) AS user_id,
    CAST(amount AS DECIMAL(20, 3)) AS metric_value
FROM
    statuses
UNION ALL
SELECT
    'assignee',
    name,
    user_id,
    CAST(amount AS DECIMAL(20, 3))
FROM
    ranked_assignees
WHERE
    rank_no <= 3
UNION ALL
SELECT
    'average_closure',
    NULL,
    NULL,
    CAST(average_seconds AS DECIMAL(20, 3))
FROM
    closure
UNION ALL
SELECT
    'comments',
    NULL,
    NULL,
    CAST(amount AS DECIMAL(20, 3))
FROM
    comments
`

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func StatsQuery(ctx context.Context, q queryer, teamID int64) (domain.Stats, error) {
	stats := domain.Stats{
		TeamID: teamID,
		ByStatus: map[string]int64{
			"todo":        0,
			"in_progress": 0,
			"done":        0,
		},
		TopAssignees: []domain.AssigneeStats{},
	}

	rows, err := q.QueryContext(ctx, StatsSQL, teamID)
	if err != nil {
		return stats, err
	}
	defer rows.Close()

	for rows.Next() {
		var metric string
		var label sql.NullString
		var uid sql.NullInt64
		var value sql.NullFloat64

		if err = rows.Scan(
			&metric,
			&label,
			&uid,
			&value,
		); err != nil {
			return stats, err
		}

		switch metric {
		case "status":
			if label.Valid && value.Valid {
				stats.ByStatus[label.String] = int64(value.Float64)
			}
		case "assignee":
			if uid.Valid && label.Valid && value.Valid {
				stats.TopAssignees = append(stats.TopAssignees, domain.AssigneeStats{
					UserID:      uid.Int64,
					Name:        label.String,
					ClosedCount: int64(value.Float64),
				})
			}
		case "average_closure":
			if value.Valid {
				v := value.Float64
				stats.AvgClosureSeconds = &v
			}
		case "comments":
			if value.Valid {
				stats.CommentsCount = int64(value.Float64)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	sort.Slice(stats.TopAssignees, func(i, j int) bool {
		left, right := stats.TopAssignees[i], stats.TopAssignees[j]
		if left.ClosedCount != right.ClosedCount {
			return left.ClosedCount > right.ClosedCount
		}
		return left.UserID < right.UserID
	})

	return stats, nil
}

func (s *Service) TeamStats(ctx context.Context, actorID, teamID int64) (domain.Stats, error) {
	if teamID <= 0 {
		return domain.Stats{}, ErrInvalid
	}

	role, err := isMember(ctx, s.DB, teamID, actorID)
	if err != nil {
		return domain.Stats{}, err
	}

	if !isManager(role) {
		return domain.Stats{}, ErrForbidden
	}

	return StatsQuery(ctx, s.DB, teamID)
}
