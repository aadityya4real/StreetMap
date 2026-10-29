package migrations

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrationsAreValidAndVersioned(t *testing.T) {
	goose.SetBaseFS(Files)
	migrationList, err := goose.CollectMigrations(".", 0, 1)
	if err != nil {
		t.Fatalf("collect embedded migrations: %v", err)
	}
	if len(migrationList) != 1 || migrationList[0].Version != 1 {
		t.Fatalf("expected one initial versioned migration, got %#v", migrationList)
	}
}
