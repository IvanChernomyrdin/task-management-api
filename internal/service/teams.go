package service

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
)

func (s *Service) CreateTeam(ctx context.Context, userID int64, name string) (team domain.Team, err error) {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > 150 {
		return team, ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return team, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	res, err := tx.ExecContext(ctx,
		"INSERT INTO teams(name,created_by,created_at) VALUES(?,?,?)",
		name, userID, now,
	)

	if err != nil {
		return team, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return team, err
	}

	if _, err = tx.ExecContext(ctx,
		"INSERT INTO team_members(team_id,user_id,role) VALUES(?,?,'owner')",
		id, userID,
	); err != nil {
		return team, err
	}

	if err = tx.Commit(); err != nil {
		return team, err
	}

	return domain.Team{
		ID:        id,
		Name:      name,
		CreatedBy: userID,
		CreatedAt: now,
		Role:      domain.RoleOwner,
	}, nil
}

func (s *Service) Teams(ctx context.Context, userID int64) ([]domain.Team, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT t.id,t.name,t.created_by,t.created_at,tm.role FROM teams t JOIN team_members tm ON tm.team_id=t.id WHERE tm.user_id=? ORDER BY t.id`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Team, 0)

	for rows.Next() {
		var t domain.Team
		if err = rows.Scan(
			&t.ID, &t.Name,
			&t.CreatedBy,
			&t.CreatedAt,
			&t.Role,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}

	return out, rows.Err()
}

func (s *Service) Invite(ctx context.Context, actorID, teamID, targetID int64, role string) error {
	if teamID <= 0 ||
		targetID <= 0 ||
		!(role == domain.RoleMember || role == domain.RoleAdmin) {
		return ErrInvalid
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var ownerID int64
	err = tx.QueryRowContext(ctx,
		"SELECT created_by FROM teams WHERE id=? FOR UPDATE",
		teamID,
	).Scan(&ownerID)

	if err == sql.ErrNoRows {
		return ErrNotFound
	}

	if err != nil {
		return err
	}

	actorRole, err := isMember(ctx, tx, teamID, actorID)
	if err != nil {
		return err
	}

	if !isManager(actorRole) {
		return ErrForbidden
	}

	if actorRole == domain.RoleAdmin && role != domain.RoleMember {
		return ErrForbidden
	}

	if targetID == ownerID {
		return ErrForbidden
	}

	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM users WHERE id=?", targetID).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}

	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO team_members(team_id,user_id,role) VALUES(?,?,?)", teamID, targetID, role)
	if duplicate(err) {
		return ErrConflict
	}

	if err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Service) ChangeRole(ctx context.Context, actorID, teamID, targetID int64, role string) error {
	if teamID <= 0 ||
		targetID <= 0 ||
		!(role == domain.RoleAdmin || role == domain.RoleMember) {
		return ErrInvalid
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var ownerID int64
	err = tx.QueryRowContext(ctx,
		"SELECT created_by FROM teams WHERE id=? FOR UPDATE",
		teamID,
	).Scan(&ownerID)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}

	if err != nil {
		return err
	}

	actorRole, err := isMember(ctx, tx, teamID, actorID)
	if err != nil {
		return err
	}

	if actorRole != domain.RoleOwner {
		return ErrForbidden
	}

	if targetID == ownerID {
		return ErrForbidden
	}

	res, err := tx.ExecContext(ctx,
		"UPDATE team_members SET role=? WHERE team_id=? AND user_id=? AND role<>'owner'",
		role, teamID, targetID)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if affected == 0 {
		var current string
		if e := tx.QueryRowContext(ctx,
			"SELECT role FROM team_members WHERE team_id=? AND user_id=?",
			teamID, targetID,
		).Scan(&current); e == sql.ErrNoRows {
			return ErrNotFound
		} else if e != nil {
			return e
		}

		if current != role {
			return ErrForbidden
		}
	}

	return tx.Commit()
}
