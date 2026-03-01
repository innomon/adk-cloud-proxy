package router

import (
	"testing"
)

func TestRegistryLookupMissing(t *testing.T) {
	r := NewRegistry()
	_, err := r.Lookup("noapp")
	if err == nil {
		t.Fatal("expected error for missing connector")
	}
}

func TestRegistryRegisterAndLookup(t *testing.T) {
	r := NewRegistry()

	// Register with a nil stream (sufficient for registry test).
	cs := r.Register("app1", nil)
	if cs == nil {
		t.Fatal("expected non-nil ConnectorStream")
	}

	found, err := r.Lookup("app1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != cs {
		t.Fatal("expected same ConnectorStream")
	}
}

func TestRegistryUnregister(t *testing.T) {
	r := NewRegistry()
	r.Register("app1", nil)
	r.Unregister("app1")

	_, err := r.Lookup("app1")
	if err == nil {
		t.Fatal("expected error after unregister")
	}
}

func TestConnectorStreamPending(t *testing.T) {
	r := NewRegistry()
	cs := r.Register("app1", nil)

	ch := cs.RegisterPending("req-1")

	// Simulate receiving a response.
	go func() {
		cs.ResolvePending("req-1", nil)
	}()

	resp := <-ch
	if resp != nil {
		t.Fatal("expected nil message for test")
	}
}
