package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func openTemp(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hahod.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, path
}

func TestCreateGetRoundtrip(t *testing.T) {
	s, _ := openTemp(t)
	created, err := s.Create("haho-1-aaaaa")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.State != StateCreated {
		t.Fatalf("state = %s, want %s", created.State, StateCreated)
	}
	got, err := s.Get("haho-1-aaaaa")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != created.ID || got.State != StateCreated {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestCreateIsNeverAnOverwrite(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Create("haho-1-aaaaa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Create("haho-1-aaaaa"); err == nil {
		t.Fatal("second create of the same id must error")
	}
}

func TestGetMissing(t *testing.T) {
	s, _ := openTemp(t)
	_, err := s.Get("haho-0-nope0")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLifecycleTransitions(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Create("haho-1-aaaaa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Transition("haho-1-aaaaa", StateQueued, func(sess *Session) { sess.JobID = "job-1" }); err != nil {
		t.Fatalf("created -> queued: %v", err)
	}
	if _, err := s.Transition("haho-1-aaaaa", StateRunning, nil); err != nil {
		t.Fatalf("queued -> running: %v", err)
	}
	landed, err := s.Transition("haho-1-aaaaa", StateLanded, func(sess *Session) {
		sess.Completion = []byte(`{"jobId":"job-1"}`)
	})
	if err != nil {
		t.Fatalf("running -> landed: %v", err)
	}
	if landed.JobID != "job-1" || len(landed.Completion) == 0 {
		t.Fatalf("landed row lost its mutations: %+v", landed)
	}
}

func TestIllegalTransitionIsLoud(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Create("haho-1-aaaaa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Transition("haho-1-aaaaa", StateLanded, nil); err == nil {
		t.Fatal("created -> landed must be refused; a session lands only from running")
	}
}

func TestSessionsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hahod.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Create("haho-1-aaaaa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	got, err := reopened.Get("haho-1-aaaaa")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}
	if got.State != StateCreated {
		t.Fatalf("state after reopen = %s, want %s", got.State, StateCreated)
	}
}
