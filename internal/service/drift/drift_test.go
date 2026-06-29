// SPDX-License-Identifier: Apache-2.0

package drift

import "testing"

func TestDetect_NoDrift(t *testing.T) {
	baseline := Distribution{"bug": 0.5, "feature": 0.3, "question": 0.2}
	current := Distribution{"bug": 0.5, "feature": 0.3, "question": 0.2}
	r := Detect(baseline, current, 0.05)
	if r.Drifted {
		t.Errorf("identical distributions should not drift, score=%.4f", r.Score)
	}
	if r.Score != 0 {
		t.Errorf("score = %.4f, want 0 for identical distributions", r.Score)
	}
}

func TestDetect_SignificantDrift(t *testing.T) {
	baseline := Distribution{"bug": 0.5, "feature": 0.3, "question": 0.2}
	current := Distribution{"bug": 0.1, "feature": 0.8, "question": 0.1}
	r := Detect(baseline, current, 0.05)
	if !r.Drifted {
		t.Errorf("major distribution shift should drift, score=%.4f", r.Score)
	}
	if r.Score <= 0.05 {
		t.Errorf("score = %.4f, should be > 0.05", r.Score)
	}
}

func TestDetect_NewCategory(t *testing.T) {
	baseline := Distribution{"bug": 0.6, "feature": 0.4}
	current := Distribution{"bug": 0.4, "feature": 0.3, "praise": 0.3}
	r := Detect(baseline, current, 0.01)
	if !r.Drifted {
		t.Errorf("new category should cause drift, score=%.4f", r.Score)
	}
	if _, ok := r.Details["praise"]; !ok {
		t.Error("details should include new category 'praise'")
	}
}

func TestDetect_EmptyDistributions(t *testing.T) {
	r := Detect(Distribution{}, Distribution{}, 0.1)
	if r.Drifted {
		t.Error("empty distributions should not drift")
	}
}
