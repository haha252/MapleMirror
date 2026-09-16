package v2

import "testing"

func TestStableMessageIDDeterministicAndDomainSeparated(t *testing.T) {
	first := StableMessageID(TypeSyncResult, "task-1", "attempt-1")
	if first == "" || len(first) != 43 {
		t.Fatalf("stable id=%q", first)
	}
	if got := StableMessageID(TypeSyncResult, "task-1", "attempt-1"); got != first {
		t.Fatalf("same domain identity changed id: %q != %q", got, first)
	}
	for _, other := range []string{
		StableMessageID(TypeSyncResult, "task-1", "attempt-2"),
		StableMessageID(TypeSyncTask, "task-1", "attempt-1"),
		StableMessageID(TypeSyncResult, "task", "1attempt-1"),
	} {
		if other == first {
			t.Fatalf("different domain identity collided with %q", first)
		}
	}
}

func TestStableMessageIDLengthPrefixAvoidsBoundaryCollision(t *testing.T) {
	left := StableMessageID(TypeAuthorizationStatus, "ab", "c")
	right := StableMessageID(TypeAuthorizationStatus, "a", "bc")
	if left == right {
		t.Fatal("component boundaries were not domain separated")
	}
}
