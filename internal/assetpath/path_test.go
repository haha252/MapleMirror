package assetpath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeRelativePathAvoidsWindowsReservedNames(t *testing.T) {
	for _, rel := range []string{
		SafeRelativePath("p", "v", "CON.zip"),
		SafeRelativePath("NUL", "v", "a.zip"),
		SafeRelativePath("p", "COM1", "a.zip"),
		SafeRelativePath("aux", "LPT9.txt", "PRN"),
	} {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			base := part
			if dot := strings.IndexByte(base, '.'); dot >= 0 {
				base = base[:dot]
			}
			if _, ok := windowsReservedNames[strings.ToUpper(base)]; ok {
				t.Fatalf("path %q contains reserved component %q", rel, part)
			}
		}
	}
}
