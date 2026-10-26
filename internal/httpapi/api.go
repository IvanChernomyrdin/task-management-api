package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
	"github.com/IvanChernomyrdin/task-management-api/internal/service"
)

type API struct {
	Service *service.Service
	Logger  *slog.Logger
}
type identityKey struct{}

func actor(r *http.Request) int64 {
	id, _ := r.Context().Value(identityKey{}).(int64)
	return id
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, service.ErrInvalid):
		status, code, message = 400, "invalid_input", "invalid input"
	case errors.Is(err, service.ErrUnauthorized):
		status, code, message = 401, "unauthorized", "invalid credentials"
	case errors.Is(err, service.ErrForbidden):
		status, code, message = 403, "forbidden", "access denied"
	case errors.Is(err, service.ErrNotFound):
		status, code, message = 404, "not_found", "resource not found"
	case errors.Is(err, service.ErrConflict):
		status, code, message = 409, "conflict", "resource already exists or version mismatch"
	case errors.Is(err, service.ErrUnavailable):
		status, code, message = 503, "unavailable", "service unavailable"
	}

	var response apiError
	response.Error.Code = code
	response.Error.Message = message

	writeJSON(w, status, response)
}

func decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return service.ErrInvalid
	}

	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return service.ErrInvalid
	}

	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return service.ErrInvalid
	}

	return nil
}

func idFromPath(r *http.Request, key string) (int64, error) {
	id, e := strconv.ParseInt(r.PathValue(key), 10, 64)
	if e != nil || id <= 0 {
		return 0, service.ErrInvalid
	}

	return id, nil
}

func getInt(q string, def, min, max int) (int, error) {
	if q == "" {
		return def, nil
	}

	n, e := strconv.Atoi(q)
	if e != nil || n < min || n > max {
		return 0, service.ErrInvalid
	}

	return n, nil
}

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			fail(w, service.ErrUnauthorized)
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))

		uid, err := a.Service.Auth.Verify(token)
		if err != nil {
			fail(w, service.ErrUnauthorized)
			return
		}

		next(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, uid)))
	}
}

func (a *API) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		a.Logger.Info(
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) { a.health(w, r) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) { a.health(w, r) })
	mux.HandleFunc("POST /api/v1/register", a.register)
	mux.HandleFunc("POST /api/v1/login", a.login)
	mux.HandleFunc("POST /api/v1/teams", a.auth(a.createTeam))
	mux.HandleFunc("GET /api/v1/teams", a.auth(a.teams))
	mux.HandleFunc("POST /api/v1/teams/{id}/invite", a.auth(a.invite))
	mux.HandleFunc("PUT /api/v1/teams/{id}/members/{user_id}/role", a.auth(a.changeRole))
	mux.HandleFunc("GET /api/v1/teams/{team_id}/stats", a.auth(a.stats))
	mux.HandleFunc("POST /api/v1/tasks", a.auth(a.createTask))
	mux.HandleFunc("GET /api/v1/tasks", a.auth(a.listTasks))
	mux.HandleFunc("PUT /api/v1/tasks/{id}", a.auth(a.updateTask))
	mux.HandleFunc("GET /api/v1/tasks/{id}/history", a.auth(a.history))
	mux.HandleFunc("POST /api/v1/tasks/{id}/comments", a.auth(a.addComment))
	mux.HandleFunc("GET /api/v1/tasks/{id}/comments", a.auth(a.comments))
	mux.HandleFunc("GET /openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		http.ServeFile(w, r, "docs/openapi.yaml")
	})
	mux.HandleFunc("GET /swagger", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, swaggerHTML)
	})
	return a.logging(mux)
}

func (a *API) health(w http.ResponseWriter, r *http.Request) {
	if err := a.Service.Ping(r.Context()); err != nil {
		a.Logger.Warn("health check failed", "error", err)
		fail(w, service.ErrUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

const swaggerHTML = `<!doctype html>
						<html lang="en">

						<head>
							<meta charset="utf-8">
							<title>Task Management API вЂ” Swagger</title>
							<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
						</head>

						<body>
							<div id="swagger-ui">
							</div>
							<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
							<script>SwaggerUIBundle({ url: '/openapi.yaml', dom_id: '#swagger-ui', persistAuthorization: true });</script>
						</body>

						</html>
					`

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}

	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	user, err := a.Service.Register(
		r.Context(),
		in.Email,
		in.Password,
		in.Name,
	)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, user)
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	token, err := a.Service.Login(
		r.Context(),
		in.Email,
		in.Password,
	)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   86400,
	})
}

func (a *API) respondErr(w http.ResponseWriter, err error) {
	if !(errors.Is(err, service.ErrInvalid) ||
		errors.Is(err, service.ErrUnauthorized) ||
		errors.Is(err, service.ErrForbidden) ||
		errors.Is(err, service.ErrNotFound) ||
		errors.Is(err, service.ErrConflict) ||
		errors.Is(err, service.ErrUnavailable)) {
		a.Logger.Error("request failed", "error", err)
	}
	fail(w, err)
}

func (a *API) createTeam(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}

	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	result, err := a.Service.CreateTeam(
		r.Context(),
		actor(r),
		in.Name,
	)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

func (a *API) teams(w http.ResponseWriter, r *http.Request) {
	out, err := a.Service.Teams(
		r.Context(),
		actor(r),
	)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}
func (a *API) invite(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r, "id")
	if err != nil {
		fail(w, err)
		return
	}

	var in struct {
		UserID int64  `json:"user_id"`
		Role   string `json:"role"`
	}

	if err = decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	if in.Role == "" {
		in.Role = domain.RoleMember
	}

	if err = a.Service.Invite(
		r.Context(),
		actor(r),
		id,
		in.UserID,
		in.Role,
	); err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"status": "invited"})
}

func (a *API) changeRole(w http.ResponseWriter, r *http.Request) {
	teamID, err := idFromPath(r, "id")
	if err != nil {
		fail(w, err)
		return
	}

	uid, err := idFromPath(r, "user_id")
	if err != nil {
		fail(w, err)
		return
	}

	var in struct {
		Role string `json:"role"`
	}

	if err = decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	if err = a.Service.ChangeRole(
		r.Context(),
		actor(r),
		teamID,
		uid,
		in.Role,
	); err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r, "team_id")
	if err != nil {
		fail(w, err)
		return
	}

	out, err := a.Service.TeamStats(
		r.Context(),
		actor(r),
		id,
	)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}
