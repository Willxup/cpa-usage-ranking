// Package admin implements the small, single-process administrator console.
package admin

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"regexp"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const SessionTTL = 8 * time.Hour
const maxSessions = 1024

var (
	ErrCredentials = errors.New("invalid administrator credentials")
	ErrSession     = errors.New("invalid administrator session")
	ErrCapacity    = errors.New("administrator session capacity reached")
	ErrInput       = errors.New("invalid administrator input")
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{2,63}$`)

// Owner is read once from the private configuration file, never stored in SQLite.
type Owner struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func ValidCredentials(username, password string) bool {
	return usernamePattern.MatchString(username) && len(password) >= 12 && len(password) <= 72
}

type Identity struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Owner    bool   `json:"owner"`
}

type Session struct {
	Identity  Identity  `json:"user"`
	CSRF      string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Service struct {
	owner     Identity
	ownerHash []byte
	mu        sync.Mutex
	sessions  map[string]Session
	now       func() time.Time
}

func New(owner Owner) (*Service, error) {
	if !ValidCredentials(owner.Username, owner.Password) {
		return nil, ErrInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(owner.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, errors.New("hash administrator password")
	}
	return &Service{owner: Identity{ID: "owner", Username: owner.Username, Owner: true}, ownerHash: hash, sessions: make(map[string]Session), now: time.Now}, nil
}

func (s *Service) Login(username, password string) (string, Session, error) {
	// Always pay the password-hash cost, including unknown usernames.
	valid := bcrypt.CompareHashAndPassword(s.ownerHash, []byte(password)) == nil
	if !valid || username != s.owner.Username {
		return "", Session{}, ErrCredentials
	}
	return s.createSession(s.owner)
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("generate administrator session")
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (s *Service) createSession(identity Identity) (string, Session, error) {
	token, err := randomToken()
	if err != nil {
		return "", Session{}, err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	for key, session := range s.sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.sessions, key)
		}
	}
	if len(s.sessions) >= maxSessions {
		return "", Session{}, ErrCapacity
	}
	session := Session{Identity: identity, CSRF: csrf, ExpiresAt: now.Add(SessionTTL)}
	s.sessions[token] = session
	return token, session, nil
}

func (s *Service) Lookup(token string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[token]
	if !ok || !s.now().Before(session.ExpiresAt) {
		delete(s.sessions, token)
		return Session{}, ErrSession
	}
	return session, nil
}

func (s *Service) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func ValidCSRF(session Session, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(session.CSRF), []byte(token)) == 1
}
