package aiimage

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Job is a single image-generation request tracked by the Store.
type Job struct {
	ID        string
	OwnerID   uuid.UUID
	Status    Status
	Result    *Result
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// clone returns a copy so callers cannot mutate the stored job out-of-band.
func (j *Job) clone() *Job {
	cp := *j
	if j.Result != nil {
		r := *j.Result
		r.Images = append([]Image(nil), j.Result.Images...)
		cp.Result = &r
	}
	return &cp
}

// Store tracks image jobs. The interface is owner-scoped: Get returns
// ErrNotFound both for unknown ids and for ids owned by a different user.
type Store interface {
	// Create records a new IN_QUEUE job owned by ownerID.
	Create(ctx context.Context, ownerID uuid.UUID) (*Job, error)
	// Get returns the job iff it exists and is owned by ownerID.
	Get(ctx context.Context, id string, ownerID uuid.UUID) (*Job, error)
	// Update applies mutate to the stored job (by id, regardless of owner —
	// the async worker owns the update) and returns the updated copy.
	Update(ctx context.Context, id string, mutate func(*Job)) (*Job, error)
}

// MemoryStore is an in-process Store with TTL eviction. A GORM-backed impl is
// a later swap behind the same interface (mirrors the image Provider pattern).
type MemoryStore struct {
	mu   sync.Mutex
	jobs map[string]*Job
	ttl  time.Duration
	now  func() time.Time
}

// MemoryStoreOption configures a MemoryStore.
type MemoryStoreOption func(*MemoryStore)

// WithTTL sets the lifetime after which a job is evicted (lazily, on access).
func WithTTL(ttl time.Duration) MemoryStoreOption {
	return func(s *MemoryStore) { s.ttl = ttl }
}

// withClock overrides the clock (test seam).
func withClock(now func() time.Time) MemoryStoreOption {
	return func(s *MemoryStore) { s.now = now }
}

const defaultJobTTL = 30 * time.Minute

// NewMemoryStore constructs a MemoryStore.
func NewMemoryStore(opts ...MemoryStoreOption) *MemoryStore {
	s := &MemoryStore{
		jobs: make(map[string]*Job),
		ttl:  defaultJobTTL,
		now:  time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Create implements Store.
func (s *MemoryStore) Create(_ context.Context, ownerID uuid.UUID) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()

	now := s.now()
	j := &Job{
		ID:        uuid.NewString(),
		OwnerID:   ownerID,
		Status:    StatusInQueue,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.jobs[j.ID] = j
	return j.clone(), nil
}

// Get implements Store with owner scoping.
func (s *MemoryStore) Get(_ context.Context, id string, ownerID uuid.UUID) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictExpiredLocked()

	j, ok := s.jobs[id]
	if !ok || j.OwnerID != ownerID {
		return nil, ErrNotFound
	}
	return j.clone(), nil
}

// Update implements Store.
func (s *MemoryStore) Update(_ context.Context, id string, mutate func(*Job)) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	j, ok := s.jobs[id]
	if !ok {
		return nil, ErrNotFound
	}
	mutate(j)
	j.UpdatedAt = s.now()
	return j.clone(), nil
}

// evictExpiredLocked drops jobs older than the TTL. Caller holds s.mu.
func (s *MemoryStore) evictExpiredLocked() {
	if s.ttl <= 0 {
		return
	}
	cutoff := s.now().Add(-s.ttl)
	for id, j := range s.jobs {
		if j.UpdatedAt.Before(cutoff) {
			delete(s.jobs, id)
		}
	}
}
