package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"remotecontrol/pkg/worker"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pkg/errors"
)

var (
	// ErrTokenNotFound indicates that the token
	// has not been found in the UserStorage.
	ErrTokenNotFound = errors.New("job has not been found")
)

// TokenHeader defines the header for token auth.
var TokenHeader = "Authorization"

// StartPayload defines a root-level JSON body for the /start request.
type StartPayload struct {
	Command []string `json:"command" binding:"required"`
}

// StopPayload defines a root-level JSON body for the /stop request.
type StopPayload struct {
	JobID string `json:"job_id" binding:"required"`
}

// UserFinder provides a way to query users by tokens.
type UserFinder interface {
	FindUser(token string) (string, error)
}

// UserStorage defines storage for user credentials.
type UserStorage struct {
	Users []User `json:"users"`
}

// User defines the structure of the user credentials.
type User struct {
	Username string `json:"username"`
	Token    string `json:"token"`
}

// NewMockStorage creates a storage with pregenerated auth data as a trade-off for this task.
// TODO: Replace it with persistent data storage with secure storing solution for tokens.
func NewMockStorage(filepath string) (UserStorage, error) {
	authFile, err := os.Open(filepath)
	if err != nil {
		fmt.Println(err)
	}
	defer authFile.Close()

	var userStorage UserStorage
	decoder := json.NewDecoder(authFile)
	err = decoder.Decode(&userStorage)
	if err != nil {
		return UserStorage{}, errors.Wrap(err, "failed to decode users file")
	}
	return userStorage, nil
}

// FindUser retrieves the username assigned to the provided token.
// TODO: Performance here does not matter as this is only a mock
// implementation. It is, of course, very bad as we iterate through all users.
// TODO: Encrypt tokens, don't store them in plain text.
func (userStorage *UserStorage) FindUser(token string) (string, error) {
	for _, user := range userStorage.Users {
		if user.Token == token {
			return user.Username, nil
		}
	}
	return "", ErrTokenNotFound
}

// Worker represents a job worker.
type Worker interface {
	Start(command []string, user string) (uuid.UUID, error)
	Stop(jobUUID uuid.UUID) error
	Query(jobUUID uuid.UUID) (*worker.Job, error)
}

func main() {
	host := flag.String("host", "0.0.0.0", "application host")
	port := flag.Int("port", 8080, "application port")
	certFile := flag.String("certFile", "", "certificate file")
	keyFile := flag.String("keyFile", "", "certificate key")
	flag.Parse()

	if len(*certFile) == 0 {
		fmt.Println("you must provide a certFile argument")
		os.Exit(0)
	}
	if len(*keyFile) == 0 {
		fmt.Println("you must provide a keyFile argument")
		os.Exit(0)
	}

	memoryStorage := worker.MemoryStorage{Data: make(map[uuid.UUID]worker.Job)}
	userStorage, err := NewMockStorage("auth/users.json")
	if err != nil {
		fmt.Printf("there was an error reading pregenerated auth data: %s", err.Error())
		os.Exit(1)
	}

	worker := worker.NewWorker(&memoryStorage)
	r := setupServer(worker, &userStorage)
	r.RunTLS(fmt.Sprintf("%s:%d", *host, *port), *certFile, *keyFile)
}

// setupServer contains all server engine logic.
func setupServer(worker Worker, userStorage UserFinder) *gin.Engine {
	r := gin.Default()
	authorized := r.Group("/api/v1/jobs")
	authorized.Use(tokenAuthMiddleware(userStorage))
	{
		authorized.POST("/start", StartJob(worker, userStorage))
		authorized.POST("/stop", StopJob(worker, userStorage))
		authorized.GET("/:job_id", QueryJob(worker, userStorage))
	}
	return r
}

// StartJob allows to start a new job in the remote system with the command
// provided in the POST body.
// TODO: There's a redundancy of the middlewares and userFromContext. I made a
// workaround with just omitting error here. But normally, it would be handled with roles.
func StartJob(jobWorker Worker, userStorage UserFinder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var startPayload StartPayload
		if err := c.ShouldBindJSON(&startPayload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		username, _ := userFromContext(c, userStorage)
		uuid, err := jobWorker.Start(startPayload.Command, username)
		if err != nil {
			c.JSON(400, gin.H{
				"error": fmt.Sprintf("could not start the job: %s", err),
			})
			return
		}

		c.JSON(202, gin.H{
			"job_id": uuid,
		})
	}
}

// StopJob allows to stop a job with ID provided in the POST body.
// It allows stopping only jobs started by the same user.
func StopJob(jobWorker Worker, userStorage UserFinder) gin.HandlerFunc {
	return func(c *gin.Context) {
		var stopPayload StopPayload
		if err := c.ShouldBindJSON(&stopPayload); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		uuid, err := uuid.Parse(stopPayload.JobID)
		if err != nil {
			c.JSON(400, gin.H{
				"error": "job_id has incorrect format",
			})
			return
		}

		job, err := jobWorker.Query(uuid)
		if err == worker.ErrJobNotFound {
			c.JSON(404, gin.H{
				"error": "job with provided id could not be found",
			})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{
				"error": fmt.Sprintf("unexpected error during data retrieval: %s", err),
			})
			return
		}

		username, _ := userFromContext(c, userStorage)
		if job.User != username {
			c.JSON(401, gin.H{
				"error": "unauthorized",
			})
			return
		}

		err = jobWorker.Stop(uuid)

		if err == worker.ErrJobNotFound {
			c.JSON(404, gin.H{
				"error": "job with provided id could not be found",
			})
			return
		}
		if err == worker.ErrJobAlreadyStopped {
			c.JSON(200, gin.H{
				"info":       "job has already been stopped",
				"stopped_at": job.StoppedAt,
			})
			return
		}
		if err == worker.ErrJobAlreadyFinished {
			c.JSON(200, gin.H{
				"info":        "job has already finished",
				"finished_at": job.FinishedAt,
			})
			return
		}
		if err != nil {
			c.JSON(400, gin.H{
				"error": fmt.Sprintf("could not stop the job: %s", err),
			})
			return
		}

		c.JSON(200, gin.H{
			"info": "stop command had been succesfully executed",
		})
	}
}

// QueryJob allows to query status and logs of the job with ID provided as
// a path parameter. It allows querying only jobs started by the same user.
// TODO: Format logs to be clear and human-readable.
// TODO: Return more information about the job.
func QueryJob(jobWorker Worker, userStorage UserFinder) gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("job_id")

		uuid, err := uuid.Parse(jobID)
		if err != nil {
			c.JSON(400, gin.H{
				"error": "job_id has incorrect format",
			})
			return
		}

		job, err := jobWorker.Query(uuid)
		if err == worker.ErrJobNotFound {
			c.JSON(404, gin.H{
				"error": "job with provided id could not be found",
			})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{
				"error": fmt.Sprintf("unexpected error during data retrieval: %s", err),
			})
			return
		}

		username, _ := userFromContext(c, userStorage)
		if job.User != username {
			c.JSON(401, gin.H{
				"error": "unauthorized",
			})
			return
		}

		c.JSON(200, gin.H{
			"status": job.Status,
			"logs":   string(job.Logs.Bytes()),
		})
	}
}

// tokenAuthMiddleware handles simple token authentication.
// TODO: Replace it with strong, secure authentication and authorization.

// Notes:
// The middleware is a little redundant, as the users are checked in the
// endpoints, but I left it here to stay true to the design document.
func tokenAuthMiddleware(userStorage UserFinder) gin.HandlerFunc {
	return func(c *gin.Context) {
		validateToken(c, userStorage)
		c.Next()
	}
}

func validateToken(c *gin.Context, userStorage UserFinder) {
	token := c.Request.Header.Get(TokenHeader)

	if token == "" {
		c.JSON(401, gin.H{
			"error": "unauthorized",
		})
		c.Abort()
	}
	_, err := userFromContext(c, userStorage)

	if err != nil {
		c.JSON(401, gin.H{
			"error": "unauthorized",
		})
		c.Abort()
	}

	c.Next()
}

func userFromContext(c *gin.Context, userStorage UserFinder) (string, error) {
	token := c.Request.Header.Get("Authorization")
	return userStorage.FindUser(token)
}
