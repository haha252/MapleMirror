package adminui

import (
	"testing"
	"time"
)

func TestDisplayTimeUsesConfiguredTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{timeLocation: loc}
	got := server.displayTime("2026-06-06T03:45:57.2495166Z")
	if got != "2026/06/06 11:45" {
		t.Fatalf("display time = %s", got)
	}
}
