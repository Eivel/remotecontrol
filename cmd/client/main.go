package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// TODO: allow to customize these options.
var timeout = 30 * time.Second
var baseURL = "api/v1/jobs"

// StartRequest defines a root-level JSON body for the start request.
type StartRequest struct {
	Command []string `json:"command"`
}

// StartResponse defines a root-level JSON body for the start response.
type StartResponse struct {
	JobID *string `json:"job_id"`
}

// StopRequest defines a root-level JSON body for the start request.
type StopRequest struct {
	JobID string `json:"job_id"`
}

// StopResponse defines a root-level JSON body for the stop response.
type StopResponse struct {
	Info       *string `json:"info"`
	StoppedAt  *string `json:"stopped_at"`
	FinishedAt *string `json:"finished_at"`
}

// QueryRequest defines a root-level JSON body for the start request.
type QueryRequest struct {
	JobID string `json:"job_id"`
}

// QueryResponse defines a root-level JSON body for the query response.
type QueryResponse struct {
	Status *string `json:"status"`
	Logs   *string `json:"logs"`
}

// ErrorResponse defines a root-level JSON body for the error response.
type ErrorResponse struct {
	Error *string `json:"error"`
}

// SuccessResponse handles universal format of the response.
type SuccessResponse struct {
	StatusCode int         `json:"code"`
	Data       interface{} `json:"data"`
}

// HTTPCaller allows to perform HTTP requests.
type HTTPCaller interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client defines http client for all requests.
// TODO: allow to set custom baseURL, handle more configuration options.
type Client struct {
	baseURL    string
	token      string
	HTTPClient HTTPCaller
}

// NewClient builds a new instance of the Client with optional certificate
// verification disabling.
// TODO: allow more options, allow to modify timeout.
func NewClient(token, host string, port int, verifyCert bool) *Client {
	client := &Client{
		baseURL: fmt.Sprintf("https://%s:%d/%s", host, port, baseURL),
		token:   token,
	}
	if !verifyCert {
		httpTransportOptions := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		client.HTTPClient = &http.Client{
			Timeout:   timeout,
			Transport: httpTransportOptions,
		}
	} else {
		client.HTTPClient = &http.Client{
			Timeout: timeout,
		}
	}

	return client
}

// StartJob allows to send start command to the API server, starting the process with
// the giben command.
// TODO: Allow for request cancellation from context when more advanced, asynchronous UI is
// implemented in the future.
func (c *Client) StartJob(ctx context.Context, command []string) (StartResponse, error) {
	body, err := json.Marshal(StartRequest{Command: command})
	if err != nil {
		return StartResponse{}, errors.Wrap(err, "error preparing Start payload")
	}

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/start", c.baseURL), bytes.NewBuffer(body))
	if err != nil {
		return StartResponse{}, errors.Wrap(err, "error preparing Start request")
	}

	request = request.WithContext(ctx)

	var startResponse StartResponse
	err = c.sendRequest(request, &startResponse)
	if err != nil {
		return StartResponse{}, errors.Wrap(err, "error sending Start request")
	}

	return startResponse, nil
}

// StopJob allows to send stop command to the API server, stopping the job gracefully.
// Keep in mind, that the stopping may take some time, as the process finishes its task
// before shutting down. It may also result in job having status Finished instead of Stopped.
func (c *Client) StopJob(ctx context.Context, jobID string) (StopResponse, error) {
	body, err := json.Marshal(StopRequest{JobID: jobID})
	if err != nil {
		return StopResponse{}, errors.Wrap(err, "error preparing Stop payload")
	}

	request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/stop", c.baseURL), bytes.NewBuffer(body))
	if err != nil {
		return StopResponse{}, errors.Wrap(err, "error preparing Stop request")
	}

	request = request.WithContext(ctx)

	var stopResponse StopResponse
	err = c.sendRequest(request, &stopResponse)
	if err != nil {
		return StopResponse{}, errors.Wrap(err, "error sending Stop request")
	}

	return stopResponse, nil
}

// QueryJob allows to send query command to the API server, fetching job status and logs.
// Logs are updated live, as the job is running.
func (c *Client) QueryJob(ctx context.Context, jobID string) (QueryResponse, error) {

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/%s", c.baseURL, jobID), nil)
	if err != nil {
		return QueryResponse{}, errors.Wrap(err, "error preparing Query request")
	}

	request = request.WithContext(ctx)

	var queryResponse QueryResponse
	err = c.sendRequest(request, &queryResponse)
	if err != nil {
		return QueryResponse{}, errors.Wrap(err, "error sending Query request")
	}

	return queryResponse, nil
}

func (c *Client) sendRequest(request *http.Request, out interface{}) error {
	request.Header.Add("Authorization", c.token)
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("Accept", "application/json; charset=utf-8")

	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusBadRequest {
		var errorResponse ErrorResponse
		err = json.NewDecoder(response.Body).Decode(&errorResponse)
		if err == nil {
			return fmt.Errorf("msg: %s, http_status_code: %d", *errorResponse.Error, response.StatusCode)
		}

		return fmt.Errorf("unknown error, status code: %d", response.StatusCode)
	}

	err = json.NewDecoder(response.Body).Decode(&out)
	if err != nil {
		return errors.Wrap(err, "error decoding response json")
	}

	return nil
}

// TODO: Use proper context handling, e.g. request cancelling.
// TODO: Rebuild flag options to avoid duplication.
func main() {
	token := os.Getenv("AUTH_TOKEN")
	if len(token) == 0 {
		fmt.Println("you must provide an AUTH_TOKEN as environment variable")
		os.Exit(1)
	}

	startCmd := flag.NewFlagSet("start", flag.ExitOnError)
	startHost := startCmd.String("host", "localhost", "application host")
	startPort := startCmd.Int("port", 8080, "application port")
	startCommandInput := startCmd.String("command", "", "command to execute on the remote server")
	startVerifyCert := startCmd.Bool("verifyCert", true, "control if client verifies server certificate (disabling not recommended)")

	stopCmd := flag.NewFlagSet("stop", flag.ExitOnError)
	stopHost := stopCmd.String("host", "localhost", "application host")
	stopPort := stopCmd.Int("port", 8080, "application port")
	stopJobID := stopCmd.String("job_id", "", "job_id of the job to stop")
	stopVerifyCert := stopCmd.Bool("verifyCert", true, "control if client verifies server certificate (disabling not recommended)")

	queryCmd := flag.NewFlagSet("query", flag.ExitOnError)
	queryHost := queryCmd.String("host", "localhost", "application host")
	queryPort := queryCmd.Int("port", 8080, "application port")
	queryJobID := queryCmd.String("job_id", "", "job_id of the job to query")
	queryVerifyCert := queryCmd.Bool("verifyCert", true, "control if client verifies server certificate (disabling not recommended)")

	if len(os.Args) < 2 {
		noCommandIssued()
	}

	ctx := context.Background()

	switch os.Args[1] {
	case "start":
		startCmd.Parse(os.Args[2:])
		if len(*startCommandInput) == 0 {
			fmt.Println("you must provide a command to run on the remote server")
			os.Exit(1)
		}
		httpClient := NewClient(token, *startHost, *startPort, *startVerifyCert)
		splitCommand := strings.Split(*startCommandInput, " ")
		response, err := httpClient.StartJob(ctx, splitCommand)
		if err != nil {
			fmt.Printf("error calling /start endpoint of the API: %s\n", err.Error())
			os.Exit(1)
		}

		if response.JobID == nil {
			fmt.Println("server did not return correct JobID")
			os.Exit(1)
		}

		fmt.Printf("job has been successfully started with id: %s\n", *response.JobID)

	case "stop":
		stopCmd.Parse(os.Args[2:])
		if len(*stopJobID) == 0 {
			fmt.Println("you must provide a job_id")
			os.Exit(1)
		}
		httpClient := NewClient(token, *stopHost, *stopPort, *stopVerifyCert)
		response, err := httpClient.StopJob(ctx, *stopJobID)
		if err != nil {
			fmt.Printf("error calling /stop endpoint of the API: %s\n", err.Error())
			os.Exit(1)
		}

		if response.Info == nil {
			fmt.Println("server did not return correct info message")
			os.Exit(1)
		}
		msg := *response.Info
		if response.StoppedAt != nil {
			msg = fmt.Sprintf("%s, Stopped At: %s", msg, *response.StoppedAt)
		} else if response.FinishedAt != nil {
			msg = fmt.Sprintf("%s, Finished At: %s", msg, *response.FinishedAt)
		}

		fmt.Println(msg)

	case "query":
		queryCmd.Parse(os.Args[2:])
		if len(*queryJobID) == 0 {
			fmt.Println("you must provide a job_id")
			os.Exit(1)
		}

		httpClient := NewClient(token, *queryHost, *queryPort, *queryVerifyCert)
		response, err := httpClient.QueryJob(ctx, *queryJobID)
		if err != nil {
			fmt.Printf("error calling /stop endpoint of the API: %s\n", err.Error())
			os.Exit(1)
		}

		if response.Status == nil {
			fmt.Println("server did not return correct job status")
			os.Exit(1)
		}

		fmt.Printf("job status: %s\nlogs: %s", *response.Status, *response.Logs)

	default:
		fmt.Println("expected 'foo' or 'bar' subcommands")
		os.Exit(1)
	}
}

func noCommandIssued() {
	fmt.Println("please provide 'start', 'stop', or 'query' arguments")
	os.Exit(1)
}
