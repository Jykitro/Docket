package events

import "testing"

func TestSubscribeBudget(t *testing.T) {
	b := NewBus()
	var cancels []func()
	for range MaxPerBus {
		_, cancel, ok := b.Subscribe()
		if !ok {
			t.Fatal("subscription under the workspace budget refused")
		}
		cancels = append(cancels, cancel)
	}
	if _, _, ok := b.Subscribe(); ok {
		t.Fatal("subscription over the workspace budget accepted")
	}
	cancels[0]()
	cancels[0]() // a second cancel must not free another slot
	_, cancel, ok := b.Subscribe()
	if !ok {
		t.Fatal("cancel did not free a slot")
	}
	if _, _, ok := b.Subscribe(); ok {
		t.Fatal("a repeated cancel freed a second slot")
	}
	cancel()
	for _, c := range cancels[1:] {
		c()
	}

	// The process-wide budget holds across buses (one per workspace).
	cancels = cancels[:0]
	for range MaxTotal / MaxPerBus {
		b := NewBus()
		for range MaxPerBus {
			_, cancel, ok := b.Subscribe()
			if !ok {
				t.Fatal("subscription under the total budget refused")
			}
			cancels = append(cancels, cancel)
		}
	}
	if _, _, ok := NewBus().Subscribe(); ok {
		t.Fatal("subscription over the total budget accepted")
	}
	cancels[0]()
	if _, cancel, ok := NewBus().Subscribe(); !ok {
		t.Fatal("cancel did not free a slot in the total budget")
	} else {
		cancel()
	}
	for _, c := range cancels[1:] {
		c()
	}
}
