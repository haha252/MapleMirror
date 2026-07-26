package public

import (
	"context"
	"testing"
)

func TestCatalogReadCompletesAfterCallerCancellation(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	store := Store{DB: db}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	projects, err := store.Projects(ctx)
	if err != nil {
		t.Fatalf("Projects after caller cancellation: %v", err)
	}
	if len(projects) != 1 || projects[0].ProjectID != "p1" {
		t.Fatalf("Projects after caller cancellation = %+v", projects)
	}
	assets, err := store.Assets(ctx, "p1")
	if err != nil {
		t.Fatalf("Assets after caller cancellation: %v", err)
	}
	if len(assets) != 1 || assets[0].AssetID != "asset-1" {
		t.Fatalf("Assets after caller cancellation = %+v", assets)
	}
	if _, err := store.routableAsset(ctx, "asset-1"); err != nil {
		t.Fatalf("routableAsset after caller cancellation: %v", err)
	}
}
