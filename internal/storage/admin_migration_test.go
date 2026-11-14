package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAdminMigrationPreservesVersionFiveData(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "ranking.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(ctx, db.Writer, []Migration{{1, createClientsV1}, {2, createModelsV2}, {3, createReportWindowV3}, {4, createDailySnapshotsV4}, {5, createPeriodStatsV5}}); err != nil {
		t.Fatal(err)
	}
	_, err = db.Writer.Exec(`INSERT INTO clients(id,token_hash,display_name,avatar_id,disabled,created_at_ms) VALUES(41,zeroblob(32),'migration-user',1,1,123);
	INSERT INTO daily_reports VALUES('2026-11-16',41,7,zeroblob(32),456);
	INSERT INTO daily_stats VALUES('2026-11-16',41,0,0,100,60,40,10,0,0,5,4,200,5,400,5);
	INSERT INTO period_stats VALUES('all','0001-01-01',0,0,41,100,60,40,10,0,0,5,4,200,5,400,5)`)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := MigrateSchema(ctx, db.Writer); err != nil {
			t.Fatal(err)
		}
	}
	var name string
	var disabled, hidden, revision, daily, period int
	if err := db.Reader.QueryRow(`SELECT c.display_name,c.disabled,c.ranking_hidden,r.revision,d.total_tokens,p.total_tokens FROM clients c JOIN daily_reports r ON r.client_id=c.id JOIN daily_stats d ON d.client_id=c.id JOIN period_stats p ON p.client_id=c.id WHERE c.id=41`).Scan(&name, &disabled, &hidden, &revision, &daily, &period); err != nil {
		t.Fatal(err)
	}
	if name != "migration-user" || disabled != 1 || hidden != 0 || revision != 7 || daily != 100 || period != 100 {
		t.Fatal("migration modified existing data")
	}
	var count int
	if err := db.Reader.QueryRow("SELECT count(*) FROM admin_accounts").Scan(&count); err != nil || count != 0 {
		t.Fatal("owner unexpectedly stored in database", err)
	}
}
