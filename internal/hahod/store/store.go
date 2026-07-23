// Package store is hahod's one store: bbolt-shaped, on disk, survivable
// (RFC section 8). It records sessions and their dispositions; harnesses and
// chutes persist nothing of their own, and nothing else on the machine keeps
// a second copy.
package store

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	bolt "go.etcd.io/bbolt"
)

// State is a session's position in its lifecycle. The transitions map below
// is the machine; a move it does not name is a bug surfaced loudly, never a
// silent overwrite.
type State string

const (
	StateCreated State = "created" // exists, quiescent, never tasked
	StateQueued  State = "queued"  // tasked, admitted, waiting on a lane slot
	StateRunning State = "running" // the chute is in flight
	StateLanded  State = "landed"  // a JobCompletion exists, terminal
)

// queued -> landed exists for exactly two loud paths: a queued job
// abandoned at shutdown, and the startup recovery sweep landing orphans as
// FAILED. A queued job that RUNS still goes through running.
var transitions = map[State][]State{
	StateCreated: {StateQueued, StateRunning},
	StateQueued:  {StateRunning, StateLanded},
	StateRunning: {StateLanded},
	StateLanded:  {},
}

// Session is one row. Completion holds the JobCompletion as protojson —
// the wire form is the stored form, so what a requestor reads back is
// byte-identical to what landed.
type Session struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	State      State     `json:"state"`
	JobID      string    `json:"job_id,omitempty"`
	Completion []byte    `json:"completion,omitempty"`
}

var bucketSessions = []byte("sessions")

type Store struct {
	db *bolt.DB
}

func Open(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketSessions)
		return err
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("store: init: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Create writes a new quiescent session. IDs are unique: a second create
// of the same ID answers ErrExists, never an overwrite — the caller
// decides whether to re-mint or fail.
func (s *Store) Create(id string) (*Session, error) {
	now := time.Now().UTC()
	sess := &Session{ID: id, CreatedAt: now, UpdatedAt: now, State: StateCreated}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSessions)
		if b.Get([]byte(id)) != nil {
			return fmt.Errorf("session %s: %w", id, ErrExists)
		}
		return putSession(b, sess)
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// List returns every session row. The recovery sweep and the (future)
// sweeper walk this; it is not a serving-path call.
func (s *Store) List() ([]*Session, error) {
	var out []*Session
	err := s.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketSessions).ForEach(func(_, raw []byte) error {
			sess := new(Session)
			if err := json.Unmarshal(raw, sess); err != nil {
				return err
			}
			out = append(out, sess)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) Get(id string) (*Session, error) {
	var sess *Session
	err := s.db.View(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bucketSessions).Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		sess = new(Session)
		return json.Unmarshal(raw, sess)
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// Transition moves a session through the state machine, applying mutate to
// the row inside the same write transaction. An unnamed transition fails
// loudly with both states in the error.
func (s *Store) Transition(id string, to State, mutate func(*Session)) (*Session, error) {
	var sess *Session
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSessions)
		raw := b.Get([]byte(id))
		if raw == nil {
			return ErrNotFound
		}
		sess = new(Session)
		if err := json.Unmarshal(raw, sess); err != nil {
			return err
		}
		if !allowed(sess.State, to) {
			return fmt.Errorf("session %s: %s -> %s: %w", id, sess.State, to, ErrIllegalTransition)
		}
		sess.State = to
		sess.UpdatedAt = time.Now().UTC()
		if mutate != nil {
			mutate(sess)
		}
		return putSession(b, sess)
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func allowed(from, to State) bool {
	return slices.Contains(transitions[from], to)
}

func putSession(b *bolt.Bucket, sess *Session) error {
	raw, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return b.Put([]byte(sess.ID), raw)
}

// Sentinels. Callers map them to their own surface: the HTTP layer answers
// 404 for ErrNotFound, 409 for ErrIllegalTransition (a racing double-task
// is the client's conflict, not a server fault), and re-mints on ErrExists.
var (
	ErrNotFound          = fmt.Errorf("store: session not found")
	ErrExists            = fmt.Errorf("store: session already exists")
	ErrIllegalTransition = fmt.Errorf("store: illegal transition")
)
