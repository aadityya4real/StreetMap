package migrations

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrationsAreValidAndVersioned(t *testing.T) {
	goose.SetBaseFS(Files)
	migrationList, err := goose.CollectMigrations(".", 0, 2)
	if err != nil {
		t.Fatalf("collect embedded migrations: %v", err)
	}
	if len(migrationList) != 2 || migrationList[0].Version != 1 || migrationList[1].Version != 2 {
		t.Fatalf("expected two versioned migrations, got %#v", migrationList)
	}
}
