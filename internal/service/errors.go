package service

import "errors"

var (
	ErrInvalid      = errors.New("invalid input")
	ErrUnauthorized = errors.New("invalid credentials")
	ErrForbidden    = errors.New("access denied")
	ErrNotFound     = errors.New("resource not found")
	ErrConflict     = errors.New("conflict")
	ErrUnavailable  = errors.New("service unavailable")
)

func CanEdit(role string, actorID, creatorID int64, assigneeID *int64, fields []string) bool {
	if role == "owner" || role == "admin" {
		return true
	}

	if role != "member" {
		return false
	}

	if actorID == creatorID {
		return true
	}

	if assigneeID != nil && *assigneeID == actorID {
		for _, field := range fields {
			if field != "status" {
				return false
			}
		}
		return true
	}

	return false
}
