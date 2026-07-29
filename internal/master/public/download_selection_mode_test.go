package public

import (
	"strings"
	"testing"
)

func TestDownloadPageIncludesConfiguredDefaultSelectionMode(t *testing.T) {
	db := openMaster(t)
	seedRoutableAsset(t, db)
	srv := Server{Store: Store{DB: db}, ProjectAssets: map[string]projectAssetConfig{
		"p1": {DefaultSelectionMode: " FILE "},
	}}

	body := string(catalogBody(t, srv))
	if !strings.Contains(body, `"default_selection_mode":"file"`) {
		t.Fatalf("expected normalized file selection mode: %s", body)
	}
}
