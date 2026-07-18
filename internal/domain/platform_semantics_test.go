package domain

import "testing"

func TestCurrentLifecycleState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		profile        string
		serviceVersion string
		want           LifecycleState
	}{
		{
			name:           "supported production release",
			profile:        "production",
			serviceVersion: "5d6ea83",
			want:           LifecycleStateSupported,
		},
		{
			name:           "blocked production dev build",
			profile:        "production",
			serviceVersion: "dev",
			want:           LifecycleStateBlocked,
		},
		{
			name:           "migrating non-production release",
			profile:        "dev",
			serviceVersion: "5d6ea83",
			want:           LifecycleStateMigrating,
		},
		{
			name:           "neutral empty runtime",
			profile:        "",
			serviceVersion: "",
			want:           LifecycleStateSupported,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := CurrentLifecycleState(tc.profile, tc.serviceVersion); got != tc.want {
				t.Fatalf("CurrentLifecycleState(%q, %q) = %q, want %q", tc.profile, tc.serviceVersion, got, tc.want)
			}
		})
	}
}

func TestRuntimeSemantics_ReturnsCanonicalVocabulary(t *testing.T) {
	t.Parallel()

	snapshot := RuntimeSemantics("production", "5d6ea83")
	if snapshot.LifecycleState != LifecycleStateSupported {
		t.Fatalf("LifecycleState = %q; want %q", snapshot.LifecycleState, LifecycleStateSupported)
	}
	if len(snapshot.Glossary) != 8 {
		t.Fatalf("Glossary length = %d; want 8", len(snapshot.Glossary))
	}
	if snapshot.Glossary[0].Key != string(PlatformTermEnvironment) {
		t.Fatalf("Glossary[0].Key = %q; want %q", snapshot.Glossary[0].Key, PlatformTermEnvironment)
	}
	if len(snapshot.CompatibilityRules) != 5 {
		t.Fatalf("CompatibilityRules length = %d; want 5", len(snapshot.CompatibilityRules))
	}
	if snapshot.CompatibilityRules[0].Key != string(CompatibilityRuleAdditive) {
		t.Fatalf("CompatibilityRules[0].Key = %q; want %q", snapshot.CompatibilityRules[0].Key, CompatibilityRuleAdditive)
	}
}

func TestLifecycleStateStringAndValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state LifecycleState
		want  string
		valid bool
	}{
		{state: LifecycleStateSupported, want: "supported", valid: true},
		{state: LifecycleStateDeprecated, want: "deprecated", valid: true},
		{state: LifecycleStateMigrating, want: "migrating", valid: true},
		{state: LifecycleStateRecovering, want: "recovering", valid: true},
		{state: LifecycleStateBlocked, want: "blocked", valid: true},
		{state: LifecycleState("unknown"), want: "unknown", valid: false},
		{state: LifecycleState(""), want: "", valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			if got := tt.state.String(); got != tt.want {
				t.Fatalf("String() = %q, want %q", got, tt.want)
			}
			if got := tt.state.Valid(); got != tt.valid {
				t.Fatalf("Valid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestPlatformGlossaryAndCompatibilityRulesReturnCopies(t *testing.T) {
	t.Parallel()

	glossary := PlatformGlossary()
	glossary[0].Label = "Mutated"

	again := PlatformGlossary()
	if again[0].Label != "Environment" {
		t.Fatalf("PlatformGlossary() returned a shared backing array; got %q", again[0].Label)
	}

	rules := CompatibilityRules()
	rules[0].Label = "Mutated"

	againRules := CompatibilityRules()
	if againRules[0].Label != "Additive" {
		t.Fatalf("CompatibilityRules() returned a shared backing array; got %q", againRules[0].Label)
	}
}
