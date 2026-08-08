// SPDX-License-Identifier: Apache-2.0

package survey

import "math"

// NPSSamplePlan is a conservative response-volume estimate. It uses the
// worst-case binomial variance and finite-population correction as a planning
// proxy; it is not an NPS confidence interval or a statistical significance
// claim.
type NPSSamplePlan struct {
	PopulationCount             int
	RequiredCompletedResponses  int
	InvitationTarget            int
	ConfidencePercent           int
	MarginOfErrorPercent        int
	ExpectedResponseRatePercent int
}

// CalculateNPSSamplePlan estimates the number of submitted responses and
// invitations needed for a proportion proxy over a finite population.
func CalculateNPSSamplePlan(population, confidence, marginOfError, expectedResponseRate int) NPSSamplePlan {
	if population <= 0 || marginOfError <= 0 || expectedResponseRate <= 0 {
		return NPSSamplePlan{}
	}
	z, ok := npsPlanningZScore(confidence)
	if !ok || marginOfError > 100 || expectedResponseRate > 100 {
		return NPSSamplePlan{}
	}
	uncorrected := (z * z * 2500) / float64(marginOfError*marginOfError)
	required := int(math.Ceil(float64(population) * uncorrected / (float64(population) + uncorrected - 1)))
	if required < 1 {
		required = 1
	}
	if required > population {
		required = population
	}
	invitations := int(math.Ceil(float64(required) * 100 / float64(expectedResponseRate)))
	if invitations < required {
		invitations = required
	}
	return NPSSamplePlan{
		PopulationCount:             population,
		RequiredCompletedResponses:  required,
		InvitationTarget:            invitations,
		ConfidencePercent:           confidence,
		MarginOfErrorPercent:        marginOfError,
		ExpectedResponseRatePercent: expectedResponseRate,
	}
}

func npsPlanningZScore(confidence int) (float64, bool) {
	switch confidence {
	case 90:
		return 1.645, true
	case 95:
		return 1.96, true
	case 99:
		return 2.576, true
	default:
		return 0, false
	}
}
