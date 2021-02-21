/*
Package worker allows to start, stop and query Linux processes as concurrent jobs.

Each job is saved to a provided storage via DataStorage interface. Its progress
is updated each time a new line in StdIn or StdErr appears.

To make use of the package create a new Worker using its constructor - NewWorker
and provide a correct DataStorage. Control the processes using Start() and Stop()
methods. Query the output anytime, even when the process is running using Query()
method.

DataStorage interface must include working data synchronization methods: Lock()
and Unlock().
*/
package worker

import (
	"bytes"
	"io"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"github.com/google/uuid"
)

var (
	// ErrJobNotFound indicates that the requested job
	// has not been found in the DataStorage.
	ErrJobNotFound = errors.New("job has not been found")

	// ErrJobAlreadyStopped indicates that the requested job
	// has already been stopped.
	ErrJobAlreadyStopped = errors.New("job has already been stopped")

	// ErrJobAlreadyFinished indicates that the requested job
	// has already been finished.
	ErrJobAlreadyFinished = errors.New("job has already been finished")
)

// DataStorage allows to store and retrieve jobs from any storage that implements the interface.
// The storage needs to implement sync.Mutex or other form of access synchronization based on
// Lock() and Unlock() methods.
type DataStorage interface {
	Store(job Job) error
	Retrieve(uuid uuid.UUID) (*Job, error)
}

// MemoryStorage is a simple, in-memory implementation of the DataStorage interface.
// Avoid using in production. Implement DataStorage with database of your choice.
type MemoryStorage struct {
	Data  map[uuid.UUID]Job
	mutex sync.Mutex
}

// Store saves the job in memory.
func (m *MemoryStorage) Store(job Job) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	job.Logs = job.Logs.Copy()
	m.Data[job.UUID] = job
	return nil
}

// Retrieve loads the job from the memory storage.
func (m *MemoryStorage) Retrieve(uuid uuid.UUID) (*Job, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	job, ok := m.Data[uuid]
	if !ok {
		return nil, ErrJobNotFound
	}
	job.Logs = job.Logs.Copy()
	return &job, nil
}

// Worker provides an interface for running, stopping and querying processes.
type Worker struct {
	DataStorage DataStorage
}

// NewWorker constructs a new Worker with a struct that implements DataStorage interface.
func NewWorker(dataStorage DataStorage) *Worker {
	return &Worker{DataStorage: dataStorage}
}

// Start creates a new Linux process with the provided command,
// instantiates a new Job object and returns its ID.
// It runs the job in a goroutine that updates its progress live.
// TODO: Add a proper logger (e.g. logrus) interface to the Worker struct to standarize logging.
// TODO: Report errors consistently in a way better than stdout, e.g. to Datadog.
// TODO: Add a optional timeout for better control over goroutines.
// TODO: Improve error handling, e.g. when job.store() fails.
//
// Notes: I generally try to avoid using goroutines in packages. It introduces unpredictable
// side effects and takes control away from the client. I couldn't come up with a better
// solution to allow this package to control StdIn and StdErr of the process.
func (worker *Worker) Start(command []string, user string) (uuid.UUID, error) {
	if len(user) == 0 {
		return uuid.Nil, errors.New("user cannot be empty")
	}
	if len(command) <= 1 {
		return uuid.Nil, errors.New("command must contain at least two elements, consider adding your own shell")
	}

	cmd := exec.Command(command[0], command[1:]...)
	combinedOutput, err := cmd.StdoutPipe()
	if err != nil {
		return uuid.Nil, errors.Wrap(err, "error creating StdoutPipe for Cmd")
	}
	cmd.Stderr = cmd.Stdout

	err = cmd.Start()
	if err != nil {
		return uuid.Nil, errors.Wrap(err, "error starting process")
	}

	job := NewJob(worker.DataStorage, command, user)
	updateLogsChan := make(chan Job, 100)

	go func() {
		job.start(updateLogsChan, cmd, combinedOutput)
	}()

	go func() {
		for j := range updateLogsChan {
			err = j.DataStorage.Store(j)
			if err != nil {
				log.Printf("job %s: error invoking Store method of the DataStorage: %s\n", j.UUID, err.Error())
			}
		}
	}()

	return job.UUID, nil
}

// Stop stops the job by its UUID and stores the timestamp.
// It returns named errors when the requested job is either not found or has already
// been stopped.
func (worker *Worker) Stop(jobUUID uuid.UUID) error {
	job, err := worker.DataStorage.Retrieve(jobUUID)
	if err != nil {
		return errors.Wrapf(err, "error retrieving job with UUID: %v", jobUUID)
	}
	if job.UUID == uuid.Nil {
		return ErrJobNotFound
	}
	if job.Status == StatusStopped {
		return ErrJobAlreadyStopped
	}
	if job.Status == StatusFinished {
		return ErrJobAlreadyFinished
	}

	err = syscall.Kill(job.PID, syscall.SIGTERM)
	if err != nil {
		return errors.Wrapf(err, "error killing the process with pid: %d", job.PID)
	}

	return err
}

// Query returns information about the job.
func (worker *Worker) Query(jobUUID uuid.UUID) (*Job, error) {
	job, err := worker.DataStorage.Retrieve(jobUUID)
	if err != nil {
		return nil, errors.Wrapf(err, "error retrieving job with UUID: %v", jobUUID)
	}
	if job.UUID == uuid.Nil {
		return nil, ErrJobNotFound
	}

	return job, nil
}

// JobStatus defines a type for job statuses.
type JobStatus string

// The constants define all possible statuses of the job.
var (
	StatusCreated  = JobStatus("created")
	StatusRunning  = JobStatus("running")
	StatusError    = JobStatus("error")
	StatusFinished = JobStatus("finished")
	StatusStopped  = JobStatus("stopped")
)

// Job is an abstraction around Linux process that contains logs and metadata.
type Job struct {
	DataStorage DataStorage
	Command     []string
	Status      JobStatus
	PID         int
	UUID        uuid.UUID
	CreatedAt   time.Time
	StartedAt   time.Time
	StoppedAt   time.Time
	FinishedAt  time.Time
	User        string
	Logs        *Buffer
}

// Buffer is used to store the logs.
type Buffer struct {
	b  bytes.Buffer
	rw sync.RWMutex
}

// NewBuffer creates a new synchronized buffer.
func NewBuffer(buf []byte) *Buffer {
	return &Buffer{b: *bytes.NewBuffer(buf)}
}

func (b *Buffer) Read(p []byte) (n int, err error) {
	b.rw.RLock()
	defer b.rw.RUnlock()
	return b.b.Read(p)
}
func (b *Buffer) Write(p []byte) (n int, err error) {
	b.rw.Lock()
	defer b.rw.Unlock()
	return b.b.Write(p)
}

// Len returns Buffer length.
func (b *Buffer) Len() int {
	b.rw.RLock()
	defer b.rw.RUnlock()
	return b.b.Len()
}

// Bytes returns Buffer content in bytes slice.
func (b *Buffer) Bytes() []byte {
	b.rw.RLock()
	defer b.rw.RUnlock()
	return b.b.Bytes()
}

// Copy creates a copy of the buffer and underlying slice.
func (b *Buffer) Copy() *Buffer {
	b.rw.RLock()
	defer b.rw.RUnlock()
	data := b.b.Bytes()
	newSlice := make([]byte, len(data))
	copy(newSlice, data)
	return &Buffer{b: *bytes.NewBuffer(newSlice)}
}

// NewJob assigns the default, required values returning new job instance.
func NewJob(dataStorage DataStorage, command []string, user string) Job {
	return Job{
		DataStorage: dataStorage,
		Status:      StatusCreated,
		Command:     command,
		UUID:        uuid.New(),
		CreatedAt:   time.Now(),
		User:        user,
		Logs:        &Buffer{},
	}
}

// start runs the the command in a new process, listens for StdIn and StdErr
// and logs them to the DataStorage.
// If the DataStorage is not accessible, the errors are raised up to the Worker.Start
// method and logged to the standard output.
// TODO: Add stopping subprocesses of the started processes.
// TODO: Add separation of the StdIn and StdErr.
func (job *Job) start(updateLogsChan chan Job, cmd *exec.Cmd, combinedOutput io.ReadCloser) {
	job.Status = StatusRunning
	job.PID = cmd.Process.Pid
	job.StartedAt = time.Now()
	updateLogsChan <- *job

	stopLoggingChan := make(chan bool)
	go job.storeOnLogsUpdate(updateLogsChan, stopLoggingChan)

	_, err := io.Copy(job.Logs, combinedOutput)
	if err != nil {
		job.Status = StatusError
		job.Logs.Write([]byte(err.Error()))
		updateLogsChan <- *job
		return
	}

	err = cmd.Wait()
	stopLoggingChan <- true

	if err != nil {
		if err.Error() == "signal: terminated" {
			job.Status = StatusStopped
			job.StoppedAt = time.Now()
			updateLogsChan <- *job
			return
		}
		job.Status = StatusError
		job.Logs.Write([]byte(err.Error()))
		updateLogsChan <- *job
		return
	}

	job.FinishedAt = time.Now()
	job.Status = StatusFinished
	updateLogsChan <- *job
	close(updateLogsChan)
}

// storeOnLogsUpdate waits for the logs buffer to grow in length and sends the logs
// for storing.
// TODO: Allow to customize the delay between checks.
func (job *Job) storeOnLogsUpdate(updateLogsChan chan Job, stopLoggingChan chan bool) {
	previousLength := job.Logs.Len()
	for {
		select {
		case <-stopLoggingChan:
			return

		default:
			if job.Logs.Len() > previousLength {
				updateLogsChan <- *job
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
}
