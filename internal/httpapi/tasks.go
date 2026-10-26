package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/IvanChernomyrdin/task-management-api/internal/domain"
	"github.com/IvanChernomyrdin/task-management-api/internal/service"
)

func (a *API) createTask(w http.ResponseWriter, r *http.Request) {
	var in service.NewTask
	if err := decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	out, err := a.Service.CreateTask(r.Context(), actor(r), in)
	if err != nil {
		a.respondErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (a *API) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	id, e := strconv.ParseInt(q.Get("team_id"), 10, 64)
	if e != nil || id <= 0 {
		fail(w, service.ErrInvalid)
		return
	}

	limit, e := getInt(q.Get("limit"), 20, 1, 100)
	if e != nil {
		fail(w, e)
		return
	}

	offset, e := getInt(q.Get("offset"), 0, 0, 1000000)
	if e != nil {
		fail(w, e)
		return
	}

	f := domain.ListFilter{
		TeamID: id,
		Status: q.Get("status"),
		Limit:  limit,
		Offset: offset,
	}

	if str := q.Get("assignee_id"); str != "" {
		assignee, e := strconv.ParseInt(str, 10, 64)
		if e != nil || assignee <= 0 {
			fail(w, service.ErrInvalid)
			return
		}
		f.AssigneeID = &assignee
	}

	result, err := a.Service.ListTasks(r.Context(), actor(r), f)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *API) updateTask(w http.ResponseWriter, r *http.Request) {
	taskID, err := idFromPath(r, "id")
	if err != nil {
		fail(w, err)
		return
	}

	var in struct {
		ExpectedVersion int64           `json:"expected_version"`
		Title           *string         `json:"title"`
		Description     *string         `json:"description"`
		Status          *string         `json:"status"`
		AssigneeID      json.RawMessage `json:"assignee_id"`
	}

	if err = decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	update := service.TaskUpdate{
		ExpectedVersion: in.ExpectedVersion,
		Title:           in.Title,
		Description:     in.Description,
		Status:          in.Status,
	}
	if in.AssigneeID != nil {
		update.AssigneeSet = true
		if string(in.AssigneeID) != "null" {
			var uid int64
			if err = json.Unmarshal(in.AssigneeID, &uid); err != nil {
				fail(w, service.ErrInvalid)
				return
			}
			update.AssigneeID = &uid
		}
	}

	result, err := a.Service.UpdateTask(
		r.Context(),
		actor(r),
		taskID,
		update,
	)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r, "id")
	if err != nil {
		fail(w, err)
		return
	}
	out, err := a.Service.TaskHistory(
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

func (a *API) addComment(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r, "id")
	if err != nil {
		fail(w, err)
		return
	}

	var in struct {
		Content string `json:"content"`
	}

	if err = decode(r, &in); err != nil {
		fail(w, err)
		return
	}

	out, err := a.Service.AddComment(r.Context(), actor(r), id, in.Content)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, out)
}

func (a *API) comments(w http.ResponseWriter, r *http.Request) {
	id, err := idFromPath(r, "id")
	if err != nil {
		fail(w, err)
		return
	}

	out, err := a.Service.Comments(r.Context(), actor(r), id)
	if err != nil {
		a.respondErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, out)
}
