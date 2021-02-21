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
