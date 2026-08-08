// SPDX-License-Identifier: Apache-2.0

package survey

import "testing"

func TestCalculateNPSSamplePlanUsesFinitePopulationCorrection(t *testing.T) {
	t.Parallel()

	plan := CalculateNPSSamplePlan(1000, 95, 10, 20)
	if plan.PopulationCount != 1000 || plan.RequiredCompletedResponses != 88 || plan.InvitationTarget != 440 {
		t.Fatalf("CalculateNPSSamplePlan() = %#v, want population 1000, 88 completes, 440 invitations", plan)
	}
}

func TestCalculateNPSSamplePlanCapsRequiredResponsesAtSmallPopulation(t *testing.T) {
	t.Parallel()

	plan := CalculateNPSSamplePlan(8, 95, 10, 20)
	if plan.RequiredCompletedResponses != 8 || plan.InvitationTarget != 40 {
		t.Fatalf("CalculateNPSSamplePlan() = %#v, want all 8 completes and 40 invitations", plan)
	}
}

func TestCalculateNPSSamplePlanRejectsUnsupportedInputs(t *testing.T) {
	t.Parallel()

	for name, input := range map[string][4]int{
		"unsupported confidence": {100, 100, 10, 20},
		"zero population":        {0, 95, 10, 20},
		"zero response rate":     {100, 95, 10, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CalculateNPSSamplePlan(input[0], input[1], input[2], input[3]); got != (NPSSamplePlan{}) {
				t.Fatalf("CalculateNPSSamplePlan() = %#v, want empty plan", got)
			}
		})
	}
}
