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
	_, err := s.Transition("haho-1-aaaaa", StateLanded, nil)
	if err == nil {
		t.Fatal("created -> landed must be refused; a session lands from running or queued only")
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("err = %v, want ErrIllegalTransition sentinel (the HTTP layer maps it to 409)", err)
	}
}

// TestQueuedLandsDirectly pins the recovery edge: a queued job abandoned at
// shutdown (or found orphaned at startup) lands FAILED without running.
func TestQueuedLandsDirectly(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Create("haho-1-aaaaa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Transition("haho-1-aaaaa", StateQueued, nil); err != nil {
		t.Fatalf("created -> queued: %v", err)
	}
	if _, err := s.Transition("haho-1-aaaaa", StateLanded, nil); err != nil {
		t.Fatalf("queued -> landed must be legal for shutdown/recovery landings: %v", err)
	}
}

func TestCreateCollisionIsTypedSentinel(t *testing.T) {
	s, _ := openTemp(t)
	if _, err := s.Create("haho-1-aaaaa"); err != nil {
		t.Fatalf("create: %v", err)
	}
	_, err := s.Create("haho-1-aaaaa")
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists sentinel (the mint loop retries on it)", err)
	}
}

func TestList(t *testing.T) {
	s, _ := openTemp(t)
	for _, id := range []string{"haho-1-aaaaa", "haho-1-bbbbb", "haho-1-ccccc"} {
		if _, err := s.Create(id); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	rows, err := s.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
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
