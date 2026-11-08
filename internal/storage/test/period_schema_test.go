package test

import (
	"context"
	"path/filepath"
	"testing"

	"cpa-usage-ranking/internal/storage"
)

func TestPeriodSchemaFreshAndV4Upgrade(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "v4"}[upgrade], func(t *testing.T) {
			db := openDatabase(t, filepath.Join(t.TempDir(), "period.db"))
			ctx := context.Background()
			if upgrade {
				if err := storage.MigrateSchema(ctx, db.Writer); err != nil {
					t.Fatal(err)
				}
				// 构造与 v4 末态相同的表及版本账本，并保留真实日快照。
				execSQL(t, db.Writer, `DROP TABLE period_stats; DELETE FROM schema_migrations WHERE version=5;
					INSERT INTO clients(token_hash,display_name,avatar_id,created_at_ms) VALUES(zeroblob(32),'one',1,0);
					INSERT INTO daily_reports VALUES('2026-01-01',1,4,zeroblob(32),0)`)
			}
			for range 2 {
				if err := storage.MigrateSchema(ctx, db.Writer); err != nil {
					t.Fatal(err)
				}
			}
			if queryInt(t, db.Reader, `SELECT count(*) FROM schema_migrations`) != 5 || queryInt(t, db.Reader, `SELECT count(*) FROM sqlite_schema WHERE name='period_stats' AND sql LIKE '%WITHOUT ROWID%'`) != 1 {
				t.Fatal("migration missing")
			}
			if upgrade && queryInt(t, db.Reader, `SELECT revision FROM daily_reports`) != 4 {
				t.Fatal("upgrade changed existing data")
			}
			if !upgrade {
				execSQL(t, db.Writer, `INSERT INTO clients(token_hash,display_name,avatar_id,created_at_ms) VALUES(zeroblob(32),'one',1,0)`)
			}
			execSQL(t, db.Writer, `INSERT INTO clients(token_hash,display_name,avatar_id,created_at_ms) VALUES(randomblob(32),'two',1,0)`)
			insert := `INSERT INTO period_stats VALUES(?,?,?,?,?,1,1,0,0,0,0,1,1,0,0,0,0)`
			for _, row := range []struct {
				kind, day           string
				model, tier, client int
			}{
				{"day", "2026-01-01", 0, 0, 1}, {"week", "2025-12-29", 0, 0, 1}, {"week", "2025-12-29", 1, 0, 1},
				{"week", "2025-12-29", 1, 1, 1}, {"week", "2025-12-29", 1, 0, 2}, {"month", "2026-01-01", 1, 0, 1}, {"all", "0001-01-01", 1, 0, 1},
			} {
				if _, err := db.Writer.Exec(insert, row.kind, row.day, row.model, row.tier, row.client); err != nil {
					t.Fatal(err)
				}
			}
			for _, row := range []struct {
				kind, day           string
				model, tier, client int
			}{
				{"day", "2026-01-01", 0, 0, 1}, {"day", "2026-01-01", 1, 0, 1}, {"week", "2025-12-29", 0, 1, 1},
				{"week", "2025-12-29", -1, 0, 1}, {"week", "2025-12-29", 1, 3, 1}, {"all", "2026-01-01", 1, 0, 1}, {"year", "2026-01-01", 1, 0, 1}, {"week", "2025-12-29", 1, 0, 99},
			} {
				if _, err := db.Writer.Exec(insert, row.kind, row.day, row.model, row.tier, row.client); err == nil {
					t.Fatalf("accepted invalid key %+v", row)
				}
			}
			if _, err := db.Writer.Exec(`UPDATE period_stats SET total_tokens=9.5 WHERE kind='day'`); err == nil {
				t.Fatal("accepted non-integer count")
			}
		})
	}
}
