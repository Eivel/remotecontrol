package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type MockHTTPClient struct {
	Response   interface{}
	StatusCode int
}

func (c *MockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	body, err := json.Marshal(c.Response)
	if err != nil {
		return nil, err
	}

	response := &http.Response{
		StatusCode: c.StatusCode,
		Body:       ioutil.NopCloser(bytes.NewReader(body)),
	}

	return response, nil
}

func TestStartJob_ok(t *testing.T) {
	jobID := "test-id"
	startResponse := &StartResponse{JobID: &jobID}
	mockHTTPClient := MockHTTPClient{
		Response:   startResponse,
		StatusCode: 200,
	}
	client := &Client{
		baseURL:    "https://mock.url",
		token:      "token",
		HTTPClient: &mockHTTPClient,
	}
	ctx := context.Background()

	response, err := client.StartJob(ctx, []string{"test", "command"})
	require.NoError(t, err)
	require.Equal(t, startResponse.JobID, response.JobID)
}

func TestStartJob_status_500(t *testing.T) {
	testError := "test error"
	mockHTTPClient := MockHTTPClient{
		Response:   &ErrorResponse{Error: &testError},
		StatusCode: 500,
	}
	client := &Client{
		baseURL:    "https://mock.url",
		token:      "token",
		HTTPClient: &mockHTTPClient,
	}
	ctx := context.Background()

	_, err := client.StartJob(ctx, []string{"test", "command"})
	require.Equal(t, "error sending Start request: msg: test error, http_status_code: 500", err.Error())
}
