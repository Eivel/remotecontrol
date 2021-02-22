package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"remotecontrol/pkg/worker"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type MockWorker struct {
	uuid uuid.UUID
	job  worker.Job
	err  error
}

func (w MockWorker) Start(command []string, user string) (uuid.UUID, error) {
	return w.uuid, w.err
}

func (w MockWorker) Stop(jobUUID uuid.UUID) error {
	return w.err
}

func (w MockWorker) Query(jobUUID uuid.UUID) (*worker.Job, error) {
	return &w.job, w.err
}

type MockFinder struct {
	token string
	user  string
	err   error
}

func (m MockFinder) FindUser(token string) (string, error) {
	if token != m.token {
		return "", errors.New("unauthorized")
	}
	return m.user, m.err
}

func TestQueryJob_unauthorized(t *testing.T) {
	userStorage := &MockFinder{}

	worker := &MockWorker{
		uuid: uuid.New(),
	}

	ts := httptest.NewServer(setupServer(worker, userStorage))
	defer ts.Close()

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/jobs/%s", ts.URL, worker.uuid))
	require.NoError(t, err)
	require.Equal(t, 401, resp.StatusCode)
}

func TestQueryJob_ok(t *testing.T) {
	userStorage := &MockFinder{
		user:  "test",
		token: "token",
	}

	worker := &MockWorker{
		job:  worker.NewJob(&worker.MemoryStorage{}, []string{"echo", "test"}, userStorage.user),
		uuid: uuid.New(),
	}
	worker.job.Logs.Write([]byte("test logs"))

	router := setupServer(worker, userStorage)

	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%s", worker.uuid), nil)
	req.Header.Add("Authorization", userStorage.token)
	require.NoError(t, err)
	router.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "{\"logs\":\"test logs\",\"status\":\"created\"}", w.Body.String())
}
