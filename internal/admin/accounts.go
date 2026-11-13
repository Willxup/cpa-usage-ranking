package admin

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

type Account struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

func accountID(id int64) string { return strconv.FormatInt(id, 10) }

func (s *Service) isOwner(actor Identity) bool {
	return actor.Owner && actor.ID == s.owner.ID && actor.Username == s.owner.Username
}

// checkActor is also used inside client-state write transactions. SQLite's
// immediate writer transactions serialize account deletion with later writes.
func (s *Service) checkActor(ctx context.Context, tx *sql.Tx, actor Identity) error {
	if s.isOwner(actor) {
		return nil
	}
	if actor.Owner {
		return ErrForbidden
	}
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM admin_accounts WHERE id = ?", actor.ID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSession
		}
		return err
	}
	return nil
}

func (s *Service) Accounts(ctx context.Context, actor Identity) (Identity, []Account, error) {
	if !s.isOwner(actor) {
		return Identity{}, nil, ErrForbidden
	}
	rows, err := s.reader.QueryContext(ctx, "SELECT id, username, created_at_ms FROM admin_accounts ORDER BY id")
	if err != nil {
		return Identity{}, nil, err
	}
	defer rows.Close()
	accounts := make([]Account, 0)
	for rows.Next() {
		var id, created int64
		var account Account
		if err := rows.Scan(&id, &account.Username, &created); err != nil {
			return Identity{}, nil, err
		}
		account.ID = accountID(id)
		account.CreatedAt = time.UnixMilli(created).UTC()
		accounts = append(accounts, account)
	}
	return s.owner, accounts, rows.Err()
}

func (s *Service) CreateAccount(ctx context.Context, actor Identity, username, password string) (Account, error) {
	if !s.isOwner(actor) {
		return Account{}, ErrForbidden
	}
	if !ValidCredentials(username, password) {
		return Account{}, ErrInput
	}
	if username == s.owner.Username {
		return Account{}, ErrConflict
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Account{}, errors.New("hash administrator password")
	}
	now := s.now().UTC().Truncate(time.Millisecond)
	result, err := s.writer.ExecContext(ctx, "INSERT INTO admin_accounts (username, password_hash, created_at_ms) VALUES (?, ?, ?)", username, hash, now.UnixMilli())
	if err != nil {
		var constraint sqlite3.Error
		if errors.As(err, &constraint) && constraint.ExtendedCode == sqlite3.ErrConstraintUnique {
			return Account{}, ErrConflict
		}
		return Account{}, err
	}
	id, err := result.LastInsertId()
	return Account{ID: accountID(id), Username: username, CreatedAt: now}, err
}

func (s *Service) DeleteAccount(ctx context.Context, actor Identity, id int64) error {
	if !s.isOwner(actor) {
		return ErrForbidden
	}
	result, err := s.writer.ExecContext(ctx, "DELETE FROM admin_accounts WHERE id = ?", id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, session := range s.sessions {
		if !session.Identity.Owner && session.Identity.ID == accountID(id) {
			delete(s.sessions, token)
		}
	}
	return nil
}
