package worker

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type MockStorage struct {
	Data  map[uuid.UUID]Job
	mutex sync.Mutex
	MemoryErrors
}

type MemoryErrors struct {
	StoreError    error
	RetrieveError error
}

func (m *MockStorage) Store(job Job) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	job.Logs = job.Logs.Copy()
	m.Data[job.UUID] = job
	return m.StoreError
}

func (m *MockStorage) Retrieve(uuid uuid.UUID) (*Job, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	job, ok := m.Data[uuid]
	if !ok {
		return nil, ErrJobNotFound
	}
	job.Logs = job.Logs.Copy()
	return &job, m.RetrieveError
}

func TestWorker_Start(t *testing.T) {
	dataStorage := &MockStorage{Data: make(map[uuid.UUID]Job)}
	worker := NewWorker(dataStorage)
	uuid, err := worker.Start([]string{"echo", "test"}, "testUser")
	require.NoError(t, err)

	var job *Job

	for {
		job, err = dataStorage.Retrieve(uuid)
		require.True(t, err == nil || err == ErrJobNotFound)
		if job != nil && job.Status == StatusFinished {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	finishedJob, err := dataStorage.Retrieve(uuid)
	require.NoError(t, err)

	require.False(t, finishedJob.StartedAt.IsZero())
	require.Equal(t, "testUser", finishedJob.User)
	require.Equal(t, "test\n", string(finishedJob.Logs.Bytes()))
}

func TestWorker_Stop(t *testing.T) {
	dataStorage := &MockStorage{Data: make(map[uuid.UUID]Job)}
	worker := NewWorker(dataStorage)
	uuid, err := worker.Start([]string{"sh", "-c", "echo started; sleep 0.1; echo finished"}, "testUser")
	require.NoError(t, err)
	var job *Job

	for {
		job, err = dataStorage.Retrieve(uuid)
		require.True(t, err == nil || err == ErrJobNotFound)
		if job != nil && len(job.Logs.Bytes()) > 0 {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	err = worker.Stop(uuid)
	require.NoError(t, err)

	require.NoError(t, err)

	for {
		job, err = dataStorage.Retrieve(uuid)
		require.True(t, err == nil || err == ErrJobNotFound)
		if job != nil && job.Status == StatusStopped {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	stoppedJob, err := dataStorage.Retrieve(uuid)
	require.NoError(t, err)

	require.Equal(t, "started\n", string(stoppedJob.Logs.Bytes()))
}

func TestWorker_Stop_twice(t *testing.T) {
	dataStorage := &MockStorage{Data: make(map[uuid.UUID]Job)}
	worker := NewWorker(dataStorage)
	uuid, err := worker.Start([]string{"sh", "-c", "echo started; sleep 0.1; echo finished"}, "testUser")
	require.NoError(t, err)
	var job *Job

	for {
		job, err = dataStorage.Retrieve(uuid)
		require.True(t, err == nil || err == ErrJobNotFound)
		if job != nil && len(job.Logs.Bytes()) > 0 {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	err = worker.Stop(uuid)
	require.NoError(t, err)

	require.NoError(t, err)

	for {
		job, err = dataStorage.Retrieve(uuid)
		require.True(t, err == nil || err == ErrJobNotFound)
		if job != nil && job.Status == StatusStopped {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	err = worker.Stop(uuid)
	require.Equal(t, ErrJobAlreadyStopped, err)

	stoppedJob, err := dataStorage.Retrieve(uuid)
	require.NoError(t, err)

	require.Equal(t, "started\n", string(stoppedJob.Logs.Bytes()))
}

func TestWorker_Stop_one_of_two(t *testing.T) {
	dataStorage := &MockStorage{Data: make(map[uuid.UUID]Job)}
	worker := NewWorker(dataStorage)
	uuid1, err := worker.Start([]string{"sh", "-c", "echo started; sleep 0.1; echo finished"}, "testUser")
	require.NoError(t, err)
	uuid2, err := worker.Start([]string{"sh", "-c", "echo started; sleep 5; echo finished"}, "testUser")
	require.NoError(t, err)
	var job1, job2 *Job

	for {
		job1, err = dataStorage.Retrieve(uuid1)
		require.True(t, err == nil || err == ErrJobNotFound)
		job2, err = dataStorage.Retrieve(uuid2)
		require.True(t, err == nil || err == ErrJobNotFound)
		if job1 != nil && job2 != nil && (len(job1.Logs.Bytes()) > 0) && (len(job2.Logs.Bytes()) > 0) {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	err = worker.Stop(uuid1)
	require.NoError(t, err)

	for {
		job1, err = dataStorage.Retrieve(uuid1)
		if job1 != nil && job1.Status == StatusStopped {
			break
		}
		time.Sleep(1 * time.Millisecond)
	}

	runningJob, err := dataStorage.Retrieve(uuid2)

	require.True(t, runningJob.StoppedAt.IsZero())
	require.Equal(t, StatusRunning, runningJob.Status)
}

func TestWorker_Stop_job_not_found(t *testing.T) {
	dataStorage := &MockStorage{
		Data: make(map[uuid.UUID]Job),
	}
	worker := NewWorker(dataStorage)
	worker.Start([]string{"sh", "-c", "echo started; sleep 0.1; echo finished"}, "testUser")

	nonExistingUUID := uuid.New()
	err := worker.Stop(nonExistingUUID)

	errMsg := fmt.Sprintf("error retrieving job with UUID: %v: job has not been found", nonExistingUUID)
	require.Equal(t, errMsg, err.Error())
}

func TestWorker_Query_created(t *testing.T) {
	dataStorage := &MockStorage{Data: make(map[uuid.UUID]Job)}
	worker := NewWorker(dataStorage)
	job := Job{
		DataStorage: dataStorage,
		Status:      StatusCreated,
		Command:     []string{"ls"},
		UUID:        uuid.New(),
		CreatedAt:   time.Now(),
		User:        "testUser",
		Logs:        &Buffer{},
	}

	dataStorage.Data[job.UUID] = job
	queryResultJob, err := worker.Query(job.UUID)

	require.NoError(t, err)
	require.Equal(t, dataStorage.Data[job.UUID].UUID, queryResultJob.UUID)
	require.Equal(t, dataStorage.Data[job.UUID].Status, queryResultJob.Status)
	require.Equal(t, dataStorage.Data[job.UUID].Command, queryResultJob.Command)
	require.Equal(t, dataStorage.Data[job.UUID].CreatedAt, queryResultJob.CreatedAt)
	require.Equal(t, dataStorage.Data[job.UUID].User, queryResultJob.User)
}

func TestWorker_Buffer_safe_write(t *testing.T) {
	dataStorage := &MockStorage{Data: make(map[uuid.UUID]Job)}
	job := NewJob(dataStorage, []string{"echo", "test"}, "test")

	job.Logs.Write([]byte("job1 logs"))
	dataStorage.Store(job)

	job2, err := dataStorage.Retrieve(job.UUID)
	require.NoError(t, err)

	job2.Logs.Write([]byte("another log"))

	require.Equal(t, []byte("job1 logs"), job.Logs.Bytes())
}
