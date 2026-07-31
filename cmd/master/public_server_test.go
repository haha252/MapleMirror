package main

import "testing"

func TestPowTelemetryDirectoryIsSiblingOfMasterLogs(t *testing.T) {
	for _, test := range []struct {
		master string
		want   string
	}{
		{master: "logs/master", want: "logs/pow"},
		{master: "/var/log/mirror/master", want: "/var/log/mirror/pow"},
		{master: "master", want: "pow"},
	} {
		if got := powTelemetryDirectory(test.master); got != test.want {
			t.Errorf("powTelemetryDirectory(%q) = %q, want %q", test.master, got, test.want)
		}
	}
}
