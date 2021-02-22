# RemoteControl

RemoteControl makes running remote Linux commands easy by providing a REST API for arbitrary actions. The application consists of three parts: worker library, API server, and CLI client.

## Worker

The package handles the implementation of direct process control. The abstraction of a process ran by the worker is called "job" in this document. The Worker allows to start, stop, and query the status and output of the job.

To make use of the package create a new Worker using its constructor - `NewWorker` and provide a correct `DataStorage`. Control the processes using `Start` and `Stop` methods. Query the output anytime, even when the process is running using `Query` method.

### Usage

```go
worker := worker.NewWorker(DataStorage: MemoryStore)
uuid := worker.Start("ls")
job, _err := worker.Query(uuid)

fmt.Prinln(job.Status)

_err = worker.Stop(uuid)
```

### DataStorage and Logs

Each job is saved to a provided storage via `DataStorage` interface. The progress of the job is updated each time a new line in StdIn or StdErr appears.

DataStorage interface must include working data synchronization methods: `Lock` and `Unlock`. Remember to use them whenever you access the storage outside of the worker package.

## API Server

The API Server creates a REST API to allow users to invoke methods defined by the Worker library.

### Endpoints

1. POST /api/v1/jobs/start

Params:
- command: String (required) - command to be run as a job on the server

The endpoint allows to start a remote process (a job) with given shell command.

2. POST /api/v1/jobs/stop

Params:
- job_id: String (required) - ID of the job to stop

The endpoint allows to stop a remote process (a job) by sending Term signal.

3. GET /api/v1/jobs/{job_id}

The endpoint allows to query a job for its status and logs.

### Middlewares

The server implements one middleware, responsible for Auth. It's an example implementation.

### TLS

The server implements TLS with server-side certificates that can be provided by params to the server binary.

Authentication is realized via tokens assigned to users. It's a trade-off for the purpose of this task, not a final solution.

## Client

Client is a CLI app that allows to run commands on the remote server via connecting to its API.

The currently handled commands relate to the endpoints that can be run:

1. start - allows to start a job

Params:
- command (String) - a command to be run on the remote host

2. stop - allows to stop a job

Params:
- job_id (String) - a job_id that follows google.UUID format

3. query - allows to query a job, returning its status and logs

Params:
- job_id (String) - a job_id that follows google.UUID format


Application-wide params:
- host (String) - API server host
- port (String) - API server port
- verifyCert (Bool) - allows to override certificate verification, for example, to test self-signed certificates

## Job

A running process and its metadata is named Job. It contains the following info:

- command
- status
- process ID
- UUID
- created at
- started at
- stopped at
- finished at
- user
- logs

Currently the options that show when the Client is used are hardcoded, in the minimalistic version for the application MVP. It is possible though, to process all of the above and reveal it to the user in the current implementation.
