package migrations

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrationsAreValidAndVersioned(t *testing.T) {
	goose.SetBaseFS(Files)
	migrationList, err := goose.CollectMigrations(".", 0, 3)
	if err != nil {
		t.Fatalf("collect embedded migrations: %v", err)
	}
	if len(migrationList) != 3 || migrationList[0].Version != 1 || migrationList[1].Version != 2 || migrationList[2].Version != 3 {
		t.Fatalf("expected three versioned migrations, got %#v", migrationList)
	}
}
