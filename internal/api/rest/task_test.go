package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sonicore/server/internal/infrastructure/task"
)

func newTaskHandler(t *testing.T) *TaskHandler {
	t.Helper()
	sched := task.NewScheduler(nil)
	require.NoError(t, sched.Register(task.Spec{
		ID:       "t-001",
		Source:   "system",
		Provider: "test",
		Name:     "test task",
		Interval: time.Hour,
	}, func(ctx context.Context) error { return nil }))
	return NewTaskHandler(sched)
}

func setTaskVars(req *http.Request, id string) *http.Request {
	return mux.SetURLVars(req, map[string]string{"id": id})
}

func TestTaskListEmpty(t *testing.T) {
	h := NewTaskHandler(task.NewScheduler(nil))

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, "[]", rec.Body.String())
}

func TestTaskListSorted(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"t-001"`)
}

func TestTaskGetFound(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.Get(rec, setTaskVars(httptest.NewRequest(http.MethodGet, "/api/tasks/t-001", nil), "t-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"t-001"`)
}

func TestTaskGetNotFound(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.Get(rec, setTaskVars(httptest.NewRequest(http.MethodGet, "/api/tasks/nope", nil), "nope"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "1100")
}

func TestTaskRunNotFound(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.Run(rec, setTaskVars(httptest.NewRequest(http.MethodPost, "/api/tasks/nope/run", nil), "nope"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "1100")
}

func TestTaskRunAccepted(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.Run(rec, setTaskVars(httptest.NewRequest(http.MethodPost, "/api/tasks/t-001/run", nil), "t-001"))

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestTaskSetEnabledMissingFieldRejected(t *testing.T) {
	h := newTaskHandler(t)

	// An absent "enabled" field must be rejected, not treated as disable.
	rec := httptest.NewRecorder()
	h.SetEnabled(rec, setTaskVars(httptest.NewRequest(http.MethodPut, "/api/tasks/t-001/enabled",
		strings.NewReader(`{}`)), "t-001"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	rec = httptest.NewRecorder()
	h.SetEnabled(rec, setTaskVars(httptest.NewRequest(http.MethodPut, "/api/tasks/t-001/enabled",
		strings.NewReader(`{"enabled":null}`)), "t-001"))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestTaskSetEnabledExplicitFalse(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.SetEnabled(rec, setTaskVars(httptest.NewRequest(http.MethodPut, "/api/tasks/t-001/enabled",
		strings.NewReader(`{"enabled":false}`)), "t-001"))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"id":"t-001","enabled":false}`, rec.Body.String())
}

func TestTaskSetEnabledNotFound(t *testing.T) {
	h := newTaskHandler(t)

	rec := httptest.NewRecorder()
	h.SetEnabled(rec, setTaskVars(httptest.NewRequest(http.MethodPut, "/api/tasks/nope/enabled",
		strings.NewReader(`{"enabled":true}`)), "nope"))

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
