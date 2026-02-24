package batch

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/nexus/provider"
)

// Service manages batch processing jobs.
type Service interface {
	// Create creates a new batch job.
	Create(ctx context.Context, input *CreateInput) (*Job, error)

	// Get returns a batch job by ID.
	Get(ctx context.Context, jobID string) (*Job, error)

	// Cancel cancels a running batch job.
	Cancel(ctx context.Context, jobID string) error

	// List returns batch jobs for a tenant.
	List(ctx context.Context, tenantID string) ([]*Job, error)
}

// Executor processes individual batch requests.
type Executor interface {
	Execute(ctx context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error)
}

// service is the default batch service implementation.
type service struct {
	executor    Executor
	concurrency int
	mu          sync.RWMutex
	jobs        map[string]*Job
}

// NewService creates a new batch service.
func NewService(executor Executor, concurrency int) Service {
	if concurrency <= 0 {
		concurrency = 10
	}
	return &service{
		executor:    executor,
		concurrency: concurrency,
		jobs:        make(map[string]*Job),
	}
}

func (s *service) Create(_ context.Context, input *CreateInput) (*Job, error) {
	job := &Job{
		ID:         generateID(),
		TenantID:   input.TenantID,
		Status:     JobPending,
		Inputs:     input.Inputs,
		TotalItems: len(input.Inputs),
		CreatedAt:  time.Now(),
		Metadata:   input.Metadata,
	}

	concurrency := s.concurrency
	if input.Concurrency > 0 && input.Concurrency < concurrency {
		concurrency = input.Concurrency
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	// Start processing in background.
	go s.process(context.Background(), job, concurrency)

	return job, nil
}

func (s *service) Get(_ context.Context, jobID string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil, nil
	}
	return job, nil
}

func (s *service) Cancel(_ context.Context, jobID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return nil
	}
	if job.Status == JobRunning || job.Status == JobPending {
		job.Status = JobCancelled
	}
	return nil
}

func (s *service) List(_ context.Context, tenantID string) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []*Job
	for _, job := range s.jobs {
		if tenantID == "" || job.TenantID == tenantID {
			result = append(result, job)
		}
	}
	return result, nil
}

func (s *service) process(ctx context.Context, job *Job, concurrency int) {
	now := time.Now()
	job.StartedAt = &now
	job.Status = JobRunning
	job.Results = make([]Result, len(job.Inputs))

	var completed atomic.Int64
	var failed atomic.Int64

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, input := range job.Inputs {
		if job.Status == JobCancelled {
			break
		}

		wg.Add(1)
		sem <- struct{}{} // acquire semaphore
		go func(idx int, inp Input) {
			defer wg.Done()
			defer func() { <-sem }() // release semaphore

			resp, err := s.executor.Execute(ctx, inp.Request)
			if err != nil {
				job.Results[idx] = Result{
					CustomID: inp.CustomID,
					Error:    &Error{Code: "execution_error", Message: err.Error()},
				}
				failed.Add(1)
			} else {
				job.Results[idx] = Result{
					CustomID: inp.CustomID,
					Response: resp,
				}
			}
			c := int(completed.Add(1))
			job.Completed = c
			job.Failed = int(failed.Load())
		}(i, input)
	}

	wg.Wait()

	completedAt := time.Now()
	job.CompletedAt = &completedAt
	if job.Status != JobCancelled {
		if job.Failed > 0 && job.Failed == job.TotalItems {
			job.Status = JobFailed
		} else {
			job.Status = JobCompleted
		}
	}
}

// generateID creates a simple batch job ID.
func generateID() string {
	return "batch_" + time.Now().Format("20060102150405")
}
