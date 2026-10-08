package tenant

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xraph/nexus/id"
)

type service struct {
	store  Store
	events Events
	inUse  InUseFunc
}

// NewService creates a new tenant service.
func NewService(store Store, opts ...Option) Service {
	s := &service{store: store}
	for _, o := range opts {
		o(s)
	}
	return s
}

// normaliseConfig returns cfg with the model names trimmed. It refuses an
// empty name, and a name that is in both lists. The input is not changed.
func normaliseConfig(cfg Config) (Config, error) {
	var err error
	if cfg.AllowedModels, err = trimModelNames("allowed_models", cfg.AllowedModels); err != nil {
		return Config{}, err
	}
	if cfg.BlockedModels, err = trimModelNames("blocked_models", cfg.BlockedModels); err != nil {
		return Config{}, err
	}
	for _, n := range cfg.BlockedModels {
		if slices.Contains(cfg.AllowedModels, n) {
			return Config{}, fmt.Errorf("%w: model %q is in both allowed_models and blocked_models", ErrInvalid, n)
		}
	}
	return cfg, nil
}

func trimModelNames(field string, names []string) ([]string, error) {
	if names == nil {
		return nil, nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			return nil, fmt.Errorf("%w: %s holds an empty model name", ErrInvalid, field)
		}
		out = append(out, n)
	}
	return out, nil
}

func (s *service) Create(ctx context.Context, input *CreateInput) (*Tenant, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	if input.Slug == "" {
		return nil, fmt.Errorf("%w: slug is required", ErrInvalid)
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
		cfg, err := normaliseConfig(*input.Config)
		if err != nil {
			return nil, err
		}
		t.Config = cfg
	}
	if t.Metadata == nil {
		t.Metadata = make(map[string]string)
	}

	if err := s.store.Insert(ctx, t); err != nil {
		return nil, err
	}
	if s.events != nil {
		s.events.EmitTenantCreated(ctx, t.ID)
	}
	return t, nil
}

func (s *service) Get(ctx context.Context, tenantID string) (*Tenant, error) {
	return s.store.FindByID(ctx, tenantID)
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
		cfg, err := normaliseConfig(*input.Config)
		if err != nil {
			return nil, err
		}
		t.Config = cfg
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

func (s *service) Delete(ctx context.Context, tenantID string) error {
	if s.inUse != nil {
		used, err := s.inUse(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("nexus: check whether tenant is in use: %w", err)
		}
		if used {
			return ErrInUse
		}
	}
	return s.store.Delete(ctx, tenantID)
}

func (s *service) List(ctx context.Context, opts *ListOptions) (*ListResult, error) {
	if opts == nil {
		opts = &ListOptions{}
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
	was := t.Status
	t.Status = status
	t.UpdatedAt = time.Now()
	if err := s.store.Update(ctx, t); err != nil {
		return err
	}
	if s.events != nil && was == StatusActive && status != StatusActive {
		s.events.EmitTenantDisabled(ctx, t.ID)
	}
	return nil
}
