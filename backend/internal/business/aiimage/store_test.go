package aiimage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMemoryStoreCreateAndOwnerScopedGet(t *testing.T) {
	s := NewMemoryStore()
	owner := uuid.New()
	other := uuid.New()

	job, err := s.Create(context.Background(), owner)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if job.Status != StatusInQueue {
		t.Fatalf("status = %q, want %q", job.Status, StatusInQueue)
	}

	got, err := s.Get(context.Background(), job.ID, owner)
	if err != nil {
		t.Fatalf("Get(owner): %v", err)
	}
	if got.ID != job.ID {
		t.Fatalf("id = %q, want %q", got.ID, job.ID)
	}

	if _, err := s.Get(context.Background(), job.ID, other); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(other) err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreGetUnknown(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.Get(context.Background(), "nope", uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreUpdate(t *testing.T) {
	s := NewMemoryStore()
	owner := uuid.New()
	job, _ := s.Create(context.Background(), owner)

	updated, err := s.Update(context.Background(), job.ID, func(j *Job) {
		j.Status = StatusCompleted
		j.Result = &Result{Images: []Image{{URL: "https://img/1"}}}
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Status != StatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", updated.Status)
	}

	// Mutating the returned copy must not affect the stored job.
	updated.Result.Images[0].URL = "tampered"
	again, _ := s.Get(context.Background(), job.ID, owner)
	if again.Result.Images[0].URL != "https://img/1" {
		t.Fatalf("stored job was mutated via returned copy: %q", again.Result.Images[0].URL)
	}
}

func TestMemoryStoreUpdateUnknown(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.Update(context.Background(), "nope", func(*Job) {}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestMemoryStoreTTLEviction(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	s := NewMemoryStore(WithTTL(time.Minute), withClock(clock))
	owner := uuid.New()

	job, _ := s.Create(context.Background(), owner)

	// Still fresh.
	if _, err := s.Get(context.Background(), job.ID, owner); err != nil {
		t.Fatalf("Get fresh: %v", err)
	}

	// Advance past the TTL; the next access lazily evicts.
	now = now.Add(2 * time.Minute)
	if _, err := s.Get(context.Background(), job.ID, owner); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get expired err = %v, want ErrNotFound", err)
	}
}
