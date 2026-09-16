package bootstrap

import (
	"os"
	"testing"
)

func TestInteractiveRejectsDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = old }()
	if Interactive() {
		t.Fatal("os.DevNull must not be treated as an interactive terminal")
	}
}
