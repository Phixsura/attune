package preflight

import "testing"

func TestRegisterAndRegistered(t *testing.T) {
	orig := registry
	defer func() { registry = orig }()

	registry = nil

	Register(Check{Name: "a", Category: CategoryConfig})
	Register(Check{Name: "b", Category: CategoryDatabase})

	got := Registered()
	if len(got) != 2 {
		t.Fatalf("Registered() = %d checks; want 2", len(got))
	}
	if got[0].Name != "a" || got[1].Name != "b" {
		t.Errorf("Registered() names = [%q, %q]; want [a, b]", got[0].Name, got[1].Name)
	}
}

func TestRegistered_ReturnsCopy(t *testing.T) {
	orig := registry
	defer func() { registry = orig }()

	registry = nil
	Register(Check{Name: "x", Category: CategoryConfig})

	got := Registered()
	got[0].Name = "mutated"
	if Registered()[0].Name != "x" {
		t.Error("Registered() returned a reference, not a copy")
	}
}
