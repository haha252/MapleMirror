package public

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"
)

func TestVDFKeyManagerUsesInjectedGeneratorAndClock(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, vdfByteSize*8)
	if err != nil {
		t.Fatal(err)
	}
	fixed := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	calls := 0
	manager, err := newVDFKeyManagerWithClock(time.Hour, nil, func() (*rsa.PrivateKey, error) {
		calls++
		return key, nil
	}, func() time.Time { return fixed })
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	material := manager.current()
	if calls != 1 || material.createdAt != fixed || len(material.encoded) != 512 || len(material.modulusID) != 43 {
		t.Fatalf("manager material calls=%d created=%s encoded=%d id=%d",
			calls, material.createdAt, len(material.encoded), len(material.modulusID))
	}
}
