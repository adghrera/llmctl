package api

import "testing"

func TestPendingApprovals(t *testing.T) {
	p := newPendingApprovals()
	ch := make(chan bool, 1)
	p.add("tok1", ch)
	if !p.resolve("tok1", true) {
		t.Fatal("resolve failed")
	}
	if got := <-ch; !got {
		t.Error("expected true")
	}
	// Second resolve of the same token fails (already removed).
	if p.resolve("tok1", false) {
		t.Error("expected second resolve to fail")
	}
	// remove is safe on missing ids.
	p.remove("nope")
}
