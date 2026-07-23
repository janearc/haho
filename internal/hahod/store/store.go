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

var transitions = map[State][]State{
	StateCreated: {StateQueued, StateRunning},
	StateQueued:  {StateRunning},
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

// Create writes a new quiescent session. Creation is idempotent on ID: a
// second create of the same ID is an error, never an overwrite.
func (s *Store) Create(id string) (*Session, error) {
	now := time.Now().UTC()
	sess := &Session{ID: id, CreatedAt: now, UpdatedAt: now, State: StateCreated}
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketSessions)
		if b.Get([]byte(id)) != nil {
			return fmt.Errorf("store: session %s already exists", id)
		}
		return putSession(b, sess)
	})
	if err != nil {
		return nil, err
	}
	return sess, nil
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
			return fmt.Errorf("store: session %s: illegal transition %s -> %s", id, sess.State, to)
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

// ErrNotFound is a sentinel: the session does not exist. Callers map it to
// their own surface (the HTTP layer answers 404).
var ErrNotFound = fmt.Errorf("store: session not found")
