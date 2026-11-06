package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Registry binds only the current validated catalog to its persistent IDs.
// Historical dictionary rows do not make removed names current again.
type Registry struct {
	catalog *Catalog
	ids     map[string]int64
}

// Register atomically registers canonical names and returns their stable IDs.
// All reads and writes in the batch use the same transaction, not the outer
// single-connection writer pool. No aliases or prices are stored in the table.
func Register(ctx context.Context, writer *sql.DB, catalog *Catalog) (*Registry, error) {
	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin model registration: %w", err)
	}
	defer tx.Rollback()
	ids := make(map[string]int64, len(catalog.models))
	for _, model := range catalog.models {
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM models WHERE model_name = ?", model.Name).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			result, insertErr := tx.ExecContext(ctx, "INSERT INTO models (model_name) VALUES (?)", model.Name)
			if insertErr != nil {
				return nil, fmt.Errorf("register model %q: %w", model.Name, insertErr)
			}
			id, err = result.LastInsertId()
		}
		if err != nil {
			return nil, fmt.Errorf("read model ID for %q: %w", model.Name, err)
		}
		ids[model.Name] = id
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit model registration: %w", err)
	}
	return &Registry{catalog: catalog, ids: ids}, nil
}

// CanonicalID looks up a canonical name in the current catalog only.
func (r *Registry) CanonicalID(name string) (int64, bool) {
	id, ok := r.ids[name]
	return id, ok
}
