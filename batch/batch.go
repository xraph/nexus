// Package batch provides batch processing for multiple LLM requests.
package batch

import (
	"time"

	"github.com/xraph/nexus/provider"
)

// JobStatus represents the state of a batch job.
type JobStatus string

const (
	JobPending   JobStatus = "pending"
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

// Job represents a batch processing job.
type Job struct {
	ID          string         `json:"id"`
	TenantID    string         `json:"tenant_id,omitempty"`
	Status      JobStatus      `json:"status"`
	Inputs      []Input        `json:"inputs"`
	Results     []Result       `json:"results,omitempty"`
	TotalItems  int            `json:"total_items"`
	Completed   int            `json:"completed"`
	Failed      int            `json:"failed"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// Input is a single item in a batch job.
type Input struct {
	CustomID string                      `json:"custom_id"`
	Request  *provider.CompletionRequest `json:"request"`
}

// Result is the outcome of a single batch item.
type Result struct {
	CustomID string                       `json:"custom_id"`
	Response *provider.CompletionResponse `json:"response,omitempty"`
	Error    *BatchError                  `json:"error,omitempty"`
}

// BatchError describes a batch item failure.
type BatchError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// CreateInput defines a new batch job.
type CreateInput struct {
	TenantID    string         `json:"tenant_id,omitempty"`
	Inputs      []Input        `json:"inputs"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Concurrency int            `json:"concurrency,omitempty"` // max parallel requests
}
