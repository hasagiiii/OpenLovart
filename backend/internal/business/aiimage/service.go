package aiimage

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// defaultJobTimeout bounds a single async generation/edit worker.
const defaultJobTimeout = 3 * time.Minute

// Service ties the Provider and Store together. It drives the async
// submit→poll lifecycle for the /api/ai/images endpoints and exposes
// synchronous Generate/Edit helpers reused by the chat agent's tools.
type Service struct {
	provider Provider
	store    Store
	timeout  time.Duration
}

// ServiceOption configures a Service.
type ServiceOption func(*Service)

// WithJobTimeout sets the per-job worker timeout.
func WithJobTimeout(d time.Duration) ServiceOption {
	return func(s *Service) { s.timeout = d }
}

// NewService constructs a Service over the given provider and store.
func NewService(provider Provider, store Store, opts ...ServiceOption) *Service {
	s := &Service{
		provider: provider,
		store:    store,
		timeout:  defaultJobTimeout,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Submit records an IN_QUEUE job owned by ownerID and runs the
// generation/edit in the background, transitioning the job through
// IN_PROGRESS → COMPLETED/FAILED. A non-empty ReferenceImage routes to Edit.
func (s *Service) Submit(ctx context.Context, ownerID uuid.UUID, p Params) (*Job, error) {
	job, err := s.store.Create(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	go s.work(job.ID, p)
	return job, nil
}

// Status returns the owner-scoped job (ErrNotFound for unknown/non-owner).
func (s *Service) Status(ctx context.Context, id string, ownerID uuid.UUID) (*Job, error) {
	return s.store.Get(ctx, id, ownerID)
}

// work runs one generation/edit on a fresh, bounded background context (the
// originating request has already returned 202, so its context is dead).
func (s *Service) work(id string, p Params) {
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	defer cancel()

	if _, err := s.store.Update(ctx, id, func(j *Job) {
		j.Status = StatusInProgress
	}); err != nil {
		return // job evicted; nothing to do
	}

	res, err := s.dispatch(ctx, p)

	_, _ = s.store.Update(ctx, id, func(j *Job) {
		if err != nil {
			j.Status = StatusFailed
			j.Error = err.Error()
			return
		}
		r := res
		j.Result = &r
		j.Status = StatusCompleted
	})
}

// dispatch routes to Edit when a reference image is present, else Generate.
func (s *Service) dispatch(ctx context.Context, p Params) (Result, error) {
	if p.ReferenceImage != "" {
		return s.provider.Edit(ctx, p)
	}
	return s.provider.Generate(ctx, p)
}

// Generate is the synchronous text-to-image helper for the chat tools.
func (s *Service) Generate(ctx context.Context, p Params) (Result, error) {
	return s.provider.Generate(ctx, p)
}

// Edit is the synchronous image-to-image helper for the chat tools.
func (s *Service) Edit(ctx context.Context, p Params) (Result, error) {
	return s.provider.Edit(ctx, p)
}
