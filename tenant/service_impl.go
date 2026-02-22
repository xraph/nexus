package tenant

import (
	"context"
	"errors"
	"time"

	"github.com/xraph/nexus/id"
)

type service struct {
	store Store
}

// NewService creates a new tenant service.
func NewService(store Store) Service {
	return &service{store: store}
}

func (s *service) Create(ctx context.Context, input *CreateInput) (*Tenant, error) {
	if input.Name == "" {
		return nil, errors.New("nexus: tenant name is required")
	}
	if input.Slug == "" {
		return nil, errors.New("nexus: tenant slug is required")
	}

	t := &Tenant{
		ID:        id.NewTenantID(),
		Name:      input.Name,
		Slug:      input.Slug,
		Status:    StatusActive,
		Metadata:  input.Metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if input.Quota != nil {
		t.Quota = *input.Quota
	}
	if input.Config != nil {
		t.Config = *input.Config
	}
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}

	if err := s.store.Insert(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *service) Get(ctx context.Context, id string) (*Tenant, error) {
	return s.store.FindByID(ctx, id)
}

func (s *service) GetBySlug(ctx context.Context, slug string) (*Tenant, error) {
	return s.store.FindBySlug(ctx, slug)
}

func (s *service) Update(ctx context.Context, tenantID string, input *UpdateInput) (*Tenant, error) {
	t, err := s.store.FindByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		t.Name = *input.Name
	}
	if input.Quota != nil {
		t.Quota = *input.Quota
	}
	if input.Config != nil {
		t.Config = *input.Config
	}
	if input.Metadata != nil {
		t.Metadata = input.Metadata
	}
	t.UpdatedAt = time.Now()

	if err := s.store.Update(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func (s *service) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

func (s *service) List(ctx context.Context, opts *ListOptions) ([]*Tenant, int, error) {
	if opts == nil {
		opts = &ListOptions{Limit: 50}
	}
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	return s.store.List(ctx, opts)
}

func (s *service) UpdateQuota(ctx context.Context, tenantID string, quota *Quota) error {
	t, err := s.store.FindByID(ctx, tenantID)
	if err != nil {
		return err
	}
	t.Quota = *quota
	t.UpdatedAt = time.Now()
	return s.store.Update(ctx, t)
}

func (s *service) SetStatus(ctx context.Context, tenantID string, status Status) error {
	t, err := s.store.FindByID(ctx, tenantID)
	if err != nil {
		return err
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	return s.store.Update(ctx, t)
}
