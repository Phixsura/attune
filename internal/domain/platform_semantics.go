// SPDX-License-Identifier: Apache-2.0

package domain

import "strings"

// PlatformTerm identifies one of the canonical platform vocabulary entries.
type PlatformTerm string

const (
	PlatformTermEnvironment    PlatformTerm = "environment"
	PlatformTermProfile        PlatformTerm = "profile"
	PlatformTermService        PlatformTerm = "service"
	PlatformTermOwner          PlatformTerm = "owner"
	PlatformTermPolicyMode     PlatformTerm = "policy_mode"
	PlatformTermReleaseState   PlatformTerm = "release_state"
	PlatformTermLifecycleState PlatformTerm = "lifecycle_state"
	PlatformTermRiskClass      PlatformTerm = "risk_class"
)

// CompatibilityRuleKey identifies one of the platform compatibility policies.
type CompatibilityRuleKey string

const (
	CompatibilityRuleAdditive           CompatibilityRuleKey = "additive"
	CompatibilityRuleBreaking           CompatibilityRuleKey = "breaking"
	CompatibilityRuleDeprecatedWithShim CompatibilityRuleKey = "deprecated_with_shim"
	CompatibilityRuleRenameWithAlias    CompatibilityRuleKey = "rename_with_alias"
	CompatibilityRuleMigrationStep      CompatibilityRuleKey = "migration_step"
)

// LifecycleState captures the support window of a runtime surface.
type LifecycleState string

const (
	LifecycleStateSupported  LifecycleState = "supported"
	LifecycleStateDeprecated LifecycleState = "deprecated"
	LifecycleStateMigrating  LifecycleState = "migrating"
	LifecycleStateRecovering LifecycleState = "recovering"
	LifecycleStateBlocked    LifecycleState = "blocked"
)

// SemanticDescriptor is the stable label + description pair used for glossary
// entries and policy summaries.
type SemanticDescriptor struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// PlatformSemantics is the current runtime-facing semantic snapshot.
type PlatformSemantics struct {
	LifecycleState     LifecycleState       `json:"lifecycleState"`
	Glossary           []SemanticDescriptor `json:"glossary"`
	CompatibilityRules []SemanticDescriptor `json:"compatibilityRules"`
}

var platformGlossary = []SemanticDescriptor{
	{
		Key:         string(PlatformTermEnvironment),
		Label:       "Environment",
		Description: "The deployment target or tenant context used by the runtime.",
	},
	{
		Key:         string(PlatformTermProfile),
		Label:       "Profile",
		Description: "The runtime mode that enables dev or production safety behavior.",
	},
	{
		Key:         string(PlatformTermService),
		Label:       "Service",
		Description: "The running product or process that owns the release.",
	},
	{
		Key:         string(PlatformTermOwner),
		Label:       "Owner",
		Description: "The team accountable for the surface and its runbook.",
	},
	{
		Key:         string(PlatformTermPolicyMode),
		Label:       "Policy mode",
		Description: "The governing mode for allow, deny, or guarded decisions.",
	},
	{
		Key:         string(PlatformTermReleaseState),
		Label:       "Release state",
		Description: "The support state attached to a version or deployable surface.",
	},
	{
		Key:         string(PlatformTermLifecycleState),
		Label:       "Lifecycle state",
		Description: "The operational state used for supported, deprecated, migrating, recovering, or blocked flows.",
	},
	{
		Key:         string(PlatformTermRiskClass),
		Label:       "Risk class",
		Description: "The severity band used to triage platform risk and operator attention.",
	},
}

var compatibilityPolicy = []SemanticDescriptor{
	{
		Key:         string(CompatibilityRuleAdditive),
		Label:       "Additive",
		Description: "Safe to add without breaking existing callers.",
	},
	{
		Key:         string(CompatibilityRuleBreaking),
		Label:       "Breaking",
		Description: "Requires an explicit migration step or version bump.",
	},
	{
		Key:         string(CompatibilityRuleDeprecatedWithShim),
		Label:       "Deprecated with shim",
		Description: "Keep both paths while callers move to the replacement.",
	},
	{
		Key:         string(CompatibilityRuleRenameWithAlias),
		Label:       "Rename with alias",
		Description: "Move the canonical name only after an alias window.",
	},
	{
		Key:         string(CompatibilityRuleMigrationStep),
		Label:       "Migration step",
		Description: "Supported only while a migration step is in flight.",
	},
}

// RuntimeSemantics returns the runtime-facing semantic snapshot for the
// current release context.
func RuntimeSemantics(profile, serviceVersion string) PlatformSemantics {
	return PlatformSemantics{
		LifecycleState:     CurrentLifecycleState(profile, serviceVersion),
		Glossary:           PlatformGlossary(),
		CompatibilityRules: CompatibilityRules(),
	}
}

// PlatformGlossary returns the canonical platform vocabulary in stable order.
func PlatformGlossary() []SemanticDescriptor {
	return cloneSemanticDescriptors(platformGlossary)
}

// CompatibilityRules returns the canonical compatibility policy descriptors in
// stable order.
func CompatibilityRules() []SemanticDescriptor {
	return cloneSemanticDescriptors(compatibilityPolicy)
}

// CurrentLifecycleState derives the current lifecycle state from the runtime
// profile and the release marker.
func CurrentLifecycleState(profile, serviceVersion string) LifecycleState {
	profile = strings.ToLower(strings.TrimSpace(profile))
	serviceVersion = strings.TrimSpace(serviceVersion)

	switch {
	case profile == "production" && (serviceVersion == "" || strings.EqualFold(serviceVersion, "dev")):
		return LifecycleStateBlocked
	case profile == "production":
		return LifecycleStateSupported
	case profile == "":
		return LifecycleStateSupported
	default:
		return LifecycleStateMigrating
	}
}

// String returns the raw lifecycle-state token.
func (s LifecycleState) String() string {
	return string(s)
}

// Valid reports whether the lifecycle state is one of the supported values.
func (s LifecycleState) Valid() bool {
	switch s {
	case LifecycleStateSupported, LifecycleStateDeprecated, LifecycleStateMigrating, LifecycleStateRecovering, LifecycleStateBlocked:
		return true
	default:
		return false
	}
}

func cloneSemanticDescriptors(in []SemanticDescriptor) []SemanticDescriptor {
	if len(in) == 0 {
		return nil
	}
	out := make([]SemanticDescriptor, len(in))
	copy(out, in)
	return out
}
