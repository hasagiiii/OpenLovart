package aiimage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeProvider is a controllable Provider for service tests.
type fakeProvider struct {
	mu          sync.Mutex
	genResult   Result
	genErr      error
	editCalled  bool
	genCalled   bool
	lastEditRef string
}

func (f *fakeProvider) Generate(_ context.Context, _ Params) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.genCalled = true
	return f.genResult, f.genErr
}

func (f *fakeProvider) Edit(_ context.Context, p Params) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editCalled = true
	f.lastEditRef = p.ReferenceImage
	return f.genResult, f.genErr
}

// waitForStatus polls the store until the job reaches a terminal status.
func waitForStatus(t *testing.T, svc *Service, id string, owner uuid.UUID) *Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, err := svc.Status(context.Background(), id, owner)
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if j.Status == StatusCompleted || j.Status == StatusFailed {
			return j
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach terminal status", id)
	return nil
}

func TestServiceSubmitCompletes(t *testing.T) {
	prov := &fakeProvider{genResult: Result{Images: []Image{{URL: "https://img/done"}}}}
	svc := NewService(prov, NewMemoryStore())
	owner := uuid.New()

	job, err := svc.Submit(context.Background(), owner, Params{Prompt: "a tree"})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if job.Status != StatusInQueue {
		t.Fatalf("initial status = %q, want IN_QUEUE", job.Status)
	}

	final := waitForStatus(t, svc, job.ID, owner)
	if final.Status != StatusCompleted {
		t.Fatalf("final status = %q, want COMPLETED", final.Status)
	}
	if final.Result == nil || final.Result.Images[0].URL != "https://img/done" {
		t.Fatalf("unexpected result: %+v", final.Result)
	}
	if !prov.genCalled || prov.editCalled {
		t.Fatalf("expected generate path only (gen=%v edit=%v)", prov.genCalled, prov.editCalled)
	}
}

func TestServiceSubmitFailure(t *testing.T) {
	prov := &fakeProvider{genErr: errors.New("boom")}
	svc := NewService(prov, NewMemoryStore())
	owner := uuid.New()

	job, _ := svc.Submit(context.Background(), owner, Params{Prompt: "x"})
	final := waitForStatus(t, svc, job.ID, owner)
	if final.Status != StatusFailed {
		t.Fatalf("status = %q, want FAILED", final.Status)
	}
	if final.Error != "boom" {
		t.Fatalf("error = %q, want boom", final.Error)
	}
}

func TestServiceSubmitRoutesEditWhenReference(t *testing.T) {
	prov := &fakeProvider{genResult: Result{Images: []Image{{URL: "https://img/edited"}}}}
	svc := NewService(prov, NewMemoryStore())
	owner := uuid.New()

	job, _ := svc.Submit(context.Background(), owner, Params{Prompt: "blue", ReferenceImage: "https://in/ref.png"})
	final := waitForStatus(t, svc, job.ID, owner)
	if final.Status != StatusCompleted {
		t.Fatalf("status = %q, want COMPLETED", final.Status)
	}
	if !prov.editCalled || prov.genCalled {
		t.Fatalf("expected edit path only (gen=%v edit=%v)", prov.genCalled, prov.editCalled)
	}
	if prov.lastEditRef != "https://in/ref.png" {
		t.Fatalf("edit ref = %q", prov.lastEditRef)
	}
}

func TestServiceSyncGenerateAndEdit(t *testing.T) {
	prov := &fakeProvider{genResult: Result{Images: []Image{{URL: "https://img/sync"}}}}
	svc := NewService(prov, NewMemoryStore())

	res, err := svc.Generate(context.Background(), Params{Prompt: "x"})
	if err != nil || res.Images[0].URL != "https://img/sync" {
		t.Fatalf("Generate sync: %v %+v", err, res)
	}
	if _, err := svc.Edit(context.Background(), Params{Prompt: "x", ReferenceImage: "r"}); err != nil {
		t.Fatalf("Edit sync: %v", err)
	}
}
