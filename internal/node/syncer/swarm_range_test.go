package syncer

import "testing"

func TestValidateSwarmContentRange(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		ok    bool
	}{
		{"exact", "bytes 2-5/10", true},
		{"wrong start", "bytes 1-5/10", false},
		{"wrong end", "bytes 2-6/10", false},
		{"wrong total", "bytes 2-5/11", false},
		{"unknown total", "bytes 2-5/*", false},
		{"wrong unit", "items 2-5/10", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSwarmContentRange(tc.value, 2, 6, 10)
			if (err == nil) != tc.ok {
				t.Fatalf("value=%q err=%v", tc.value, err)
			}
		})
	}
}
