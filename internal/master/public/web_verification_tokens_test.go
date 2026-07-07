package public

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"mirror-server/internal/downloadtoken"
)

func TestWebVerificationTokenStoreHashesAndConsumesOnce(t *testing.T) {
	store := newWebVerificationTokenStore(10, 10*time.Minute)
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	token, err := store.issue("asset-1", "192.0.2.9/32", now)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != downloadtoken.OpaqueBytes {
		t.Fatalf("verification token should use opaque format: len=%d err=%v", len(decoded), err)
	}
	if _, ok := store.entries[token]; ok {
		t.Fatal("raw verification token must not be retained")
	}
	if _, ok := store.entries[downloadtoken.OpaqueHash(token)]; !ok {
		t.Fatal("verification token hash should be retained")
	}
	if store.consume(token, "asset-1", "198.51.100.8/32", now.Add(time.Second)) {
		t.Fatal("token bound to another client should not be accepted")
	}
	if !store.consume(token, "asset-1", "192.0.2.9/32", now.Add(2*time.Second)) {
		t.Fatal("valid token should be accepted")
	}
	if store.consume(token, "asset-1", "192.0.2.9/32", now.Add(3*time.Second)) {
		t.Fatal("consumed token should not be accepted twice")
	}
}

func TestWebVerificationTokenStoreExpiryAndCapacity(t *testing.T) {
	store := newWebVerificationTokenStore(2, time.Minute)
	now := time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
	first, _ := store.issue("asset-1", "192.0.2.1/32", now)
	second, _ := store.issue("asset-1", "192.0.2.2/32", now.Add(time.Second))
	third, _ := store.issue("asset-1", "192.0.2.3/32", now.Add(2*time.Second))
	if store.consume(first, "asset-1", "192.0.2.1/32", now.Add(3*time.Second)) {
		t.Fatal("oldest token should be evicted at capacity")
	}
	if !store.consume(second, "asset-1", "192.0.2.2/32", now.Add(3*time.Second)) ||
		!store.consume(third, "asset-1", "192.0.2.3/32", now.Add(3*time.Second)) {
		t.Fatal("newer tokens should remain available")
	}
	expired, _ := store.issue("asset-1", "192.0.2.4/32", now)
	if store.consume(expired, "asset-1", "192.0.2.4/32", now.Add(time.Minute)) {
		t.Fatal("token should expire at its deadline")
	}
}

func TestWebVerificationButtonsUseOneLabelFromEachGroup(t *testing.T) {
	for i := 0; i < 50; i++ {
		buttons := newWebVerificationButtons()
		if len(buttons) != len(webVerificationLabelGroups) {
			t.Fatalf("button count=%d want %d", len(buttons), len(webVerificationLabelGroups))
		}
		seen := make(map[int]bool)
		intents := make(map[string]bool)
		for _, button := range buttons {
			group := verificationLabelGroup(button.Label)
			if group < 0 || seen[group] {
				t.Fatalf("buttons should contain one label per group: %+v", buttons)
			}
			seen[group] = true
			if button.Intent == "" || intents[button.Intent] {
				t.Fatalf("button intents should be non-empty and unique: %+v", buttons)
			}
			intents[button.Intent] = true
		}
	}
}

func TestWebVerificationButtonsShuffleAndFallback(t *testing.T) {
	zeroes := webVerificationButtons(bytes.NewReader(make([]byte, 128)))
	ones := webVerificationButtons(bytes.NewReader(bytes.Repeat([]byte{1}, 128)))
	if labelsEqual(zeroes, ones) {
		t.Fatalf("different random input should change labels/order: zero=%+v one=%+v", zeroes, ones)
	}
	fallback := webVerificationButtons(errorReader{})
	if !labelsEqual(fallback, fallbackWebVerificationButtons()) {
		t.Fatalf("unexpected random failure fallback: %+v", fallback)
	}
}

func verificationLabelGroup(label string) int {
	for group, labels := range webVerificationLabelGroups {
		for _, candidate := range labels {
			if candidate == label {
				return group
			}
		}
	}
	return -1
}

func labelsEqual(left, right []webVerificationButton) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Label != right[i].Label {
			return false
		}
	}
	return true
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("random unavailable") }
