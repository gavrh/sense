package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	singlePath = "/v1/systemone"
	batchPath  = "/v1/systemone/batch"
	healthPath = "/health"

	maxAttempts = 3
)

type Client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

func New(baseURL, apiKey, model string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: timeout},
	}
}

type NoulLabels struct {
	False string `json:"false"`
	True  string `json:"true"`
}

type Question struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`

	// object for "choice", array for "score", optional for "noul"
	Criteria any         `json:"criteria,omitempty"`
	Labels   *NoulLabels `json:"labels,omitempty"`
}

type Request struct {
	State         any                 `json:"state"`
	Questions     map[string]Question `json:"questions"`
	Model         string              `json:"model,omitempty"`
	Task          string              `json:"task,omitempty"`
	Lang          string              `json:"lang,omitempty"`
	MinConfidence *float64            `json:"min_confidence,omitempty"`
	MaxLen        *int                `json:"max_len,omitempty"`
	HeadMaxLen    *int                `json:"head_max_len,omitempty"`
}

type BatchRequest struct {
	States        []any               `json:"states"`
	Questions     map[string]Question `json:"questions"`
	Model         string              `json:"model,omitempty"`
	MaxLen        *int                `json:"max_len,omitempty"`
	HeadMaxLen    *int                `json:"head_max_len,omitempty"`
	Task          string              `json:"task,omitempty"`
	Lang          string              `json:"lang,omitempty"`
	LangGuess     string              `json:"lang_guess,omitempty"`
	MinConfidence *float64            `json:"min_confidence,omitempty"`
	BatchSize     *int                `json:"batch_size,omitempty"`
	SortByLength  bool                `json:"sort_by_length,omitempty"`
}

type Answer struct {
	// JSON type varies by question type; use the As* helpers to read it
	Answer json.RawMessage `json:"answer"`

	Confidence       *float64 `json:"confidence,omitempty"`
	AnswerConfidence *float64 `json:"answer_confidence,omitempty"`
	Action           string   `json:"action,omitempty"`
}

func (a Answer) AsString() (string, error) {
	var value string
	if err := json.Unmarshal(a.Answer, &value); err != nil {
		return "", fmt.Errorf("jev: answer is not a string: %w", err)
	}
	return value, nil
}

func (a Answer) AsBool() (bool, error) {
	var value bool
	if err := json.Unmarshal(a.Answer, &value); err != nil {
		return false, fmt.Errorf("jev: answer is not a bool: %w", err)
	}
	return value, nil
}

func (a Answer) AsInt() (int, error) {
	var value int
	if err := json.Unmarshal(a.Answer, &value); err != nil {
		return 0, fmt.Errorf("jev: answer is not an int: %w", err)
	}
	return value, nil
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type Response struct {
	Model   string            `json:"model"`
	Answers map[string]Answer `json:"answers"`
	Usage   Usage             `json:"usage"`
	Routing map[string]any    `json:"routing,omitempty"`
}

type BatchResponse struct {
	Results    []Response `json:"results"`
	TotalUsage Usage      `json:"total_usage"`
}

type Health struct {
	Status string   `json:"status"`
	Loaded []string `json:"loaded"`
	Device string   `json:"device"`
}

type APIError struct {
	StatusCode int
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("jev: server returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("jev: server returned status %d: %s", e.StatusCode, e.Detail)
}

func (c *Client) SystemOne(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	var resp Response
	if err := c.send(ctx, singlePath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) SystemOneBatch(ctx context.Context, req BatchRequest) (*BatchResponse, error) {
	if req.Model == "" {
		req.Model = c.model
	}

	var resp BatchResponse
	if err := c.send(ctx, batchPath, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) Health(ctx context.Context) (*Health, error) {
	data, _, err := c.roundTrip(ctx, http.MethodGet, healthPath, nil)
	if err != nil {
		return nil, err
	}

	var health Health
	if err := json.Unmarshal(data, &health); err != nil {
		return nil, fmt.Errorf("jev: decode health: %w", err)
	}
	return &health, nil
}

// send retries 503 responses up to maxAttempts.
func (c *Client) send(ctx context.Context, path string, payload, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jev: encode request: %w", err)
	}

	for attempt := 1; ; attempt++ {
		data, header, err := c.roundTrip(ctx, http.MethodPost, path, body)
		if err == nil {
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("jev: decode response: %w", err)
			}
			return nil
		}

		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusServiceUnavailable && attempt < maxAttempts {
			if err := wait(ctx, retryDelay(header)); err != nil {
				return err
			}
			continue
		}
		return err
	}
}

// roundTrip returns the response headers even on error so Retry-After can be read.
func (c *Client) roundTrip(ctx context.Context, method, path string, body []byte) ([]byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("jev: request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.Header, fmt.Errorf("jev: read response: %w", err)
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, resp.Header, parseAPIError(resp.StatusCode, data)
	}
	return data, resp.Header, nil
}

func retryDelay(header http.Header) time.Duration {
	const fallback = time.Second

	if header == nil {
		return fallback
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return fallback
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// parseAPIError reads {"detail": ...}, which may be a string or a structured
// validation error, and falls back to the raw body.
func parseAPIError(statusCode int, body []byte) error {
	var payload struct {
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && len(payload.Detail) > 0 {
		var detail string
		if err := json.Unmarshal(payload.Detail, &detail); err == nil {
			return &APIError{StatusCode: statusCode, Detail: detail}
		}
		return &APIError{StatusCode: statusCode, Detail: string(payload.Detail)}
	}
	return &APIError{StatusCode: statusCode, Detail: string(body)}
}
