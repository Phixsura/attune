// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"math"
	"testing"
)

func defCfg() DetectorConfig {
	return DetectorConfig{ZThreshold: 2.5, MinCount: 10, MinBaselinePoints: 4}
}

func TestDetectSpikeOnQuietBaseline(t *testing.T) {
	// All-zero baseline: med=0, mad=0, sigma floors at 1.0 → z = 15.
	v := Detect([]int64{0, 0, 0, 0, 0, 0, 0, 0}, 15, defCfg())
	if v.Direction != DirectionSpike {
		t.Fatalf("want spike, got %q (z=%f)", v.Direction, v.Z)
	}
	if v.Z != 15 {
		t.Fatalf("want z=15, got %f", v.Z)
	}
}

func TestDetectLowCountDoublingIsQuiet(t *testing.T) {
	// med=3; observed 6 passes the 2*med multiplier but z=(6-3)/sigma stays
	// under 2.5 with sigma=max(mad/0.6745, sqrt(3), 1)=sqrt(3)≈1.73 → z≈1.73.
	v := Detect([]int64{3, 2, 4, 3}, 6, defCfg())
	if v.Direction != "" {
		t.Fatalf("low-count doubling must not fire, got %q z=%f", v.Direction, v.Z)
	}
}

func TestDetectMinCountGuardBlocksTinySpike(t *testing.T) {
	// z is enormous but observed=8 < MinCount=10 → no verdict.
	v := Detect([]int64{0, 0, 0, 0}, 8, defCfg())
	if v.Direction != "" {
		t.Fatalf("min-count guard failed, got %q", v.Direction)
	}
}

func TestDetectMultiplierGuardBlocksTrendGrowth(t *testing.T) {
	// Steady growth: baseline med=100, observed 150 → z=(150-100)/10=5 ≥ 2.5
	// but 150 < 2*100 → multiplier guard blocks (sigma=sqrt(100)=10; MAD small).
	v := Detect([]int64{90, 95, 100, 105, 100, 98, 102, 100}, 150, defCfg())
	if v.Direction != "" {
		t.Fatalf("multiplier guard failed, got %q z=%f", v.Direction, v.Z)
	}
}

func TestDetectSpikeOnConstantBaseline(t *testing.T) {
	// MAD=0 → sigma=sqrt(10)≈3.162; z=(25-10)/3.162≈4.74 ≥ 2.5; 25 ≥ 2*10.
	v := Detect([]int64{10, 10, 10, 10}, 25, defCfg())
	if v.Direction != DirectionSpike {
		t.Fatalf("want spike, got %q z=%f", v.Direction, v.Z)
	}
	if math.Abs(v.Z-4.743) > 0.01 {
		t.Fatalf("want z≈4.743, got %f", v.Z)
	}
}

func TestDetectDropToZero(t *testing.T) {
	// med=12 ≥ 5, observed 0 → z=(0-12)/sqrt(12)≈-3.46 ≤ -2.5 → drop.
	v := Detect([]int64{12, 11, 13, 12, 12, 11, 12, 13}, 0, defCfg())
	if v.Direction != DirectionDrop {
		t.Fatalf("want drop, got %q z=%f", v.Direction, v.Z)
	}
}

func TestDetectDropGuardOnLowBaseline(t *testing.T) {
	// med=3 < 5 → drop never fires even at observed 0.
	v := Detect([]int64{3, 3, 4, 3}, 0, defCfg())
	if v.Direction != "" {
		t.Fatalf("drop guard failed, got %q", v.Direction)
	}
}

func TestDetectInsufficientBaseline(t *testing.T) {
	v := Detect([]int64{5, 6, 7}, 100, defCfg())
	if !v.Insufficient || v.Direction != "" {
		t.Fatalf("want insufficient+quiet, got insufficient=%v direction=%q", v.Insufficient, v.Direction)
	}
}

func TestDetectExpectedBand(t *testing.T) {
	// baseline [10,10,10,10]: med=10, sigma=sqrt(10). Band = med ± Z*sigma.
	v := Detect([]int64{10, 10, 10, 10}, 25, defCfg())
	wantHigh := 10 + 2.5*math.Sqrt(10)
	if math.Abs(v.ExpectedHigh-wantHigh) > 0.001 || v.ExpectedMed != 10 {
		t.Fatalf("band wrong: med=%f high=%f (want high %f)", v.ExpectedMed, v.ExpectedHigh, wantHigh)
	}
	// Low clipped at 0 when Z*sigma > med.
	v2 := Detect([]int64{1, 1, 1, 1}, 30, defCfg())
	if v2.ExpectedLow != 0 {
		t.Fatalf("expected low clipped to 0, got %f", v2.ExpectedLow)
	}
}

func TestDetectDeterminism(t *testing.T) {
	b := []int64{7, 3, 9, 5, 6, 4, 8, 5}
	first := Detect(b, 40, defCfg())
	for i := 0; i < 10; i++ {
		if got := Detect(b, 40, defCfg()); got != first {
			t.Fatalf("nondeterministic: %+v vs %+v", got, first)
		}
	}
}

func TestDetectDoesNotMutateBaseline(t *testing.T) {
	b := []int64{9, 1, 5, 3}
	Detect(b, 40, defCfg())
	if b[0] != 9 || b[1] != 1 || b[2] != 5 || b[3] != 3 {
		t.Fatalf("baseline slice mutated: %v", b)
	}
}

func TestZThresholdFor(t *testing.T) {
	cases := map[string]float64{
		SensitivityHigh:   2.0,
		SensitivityMedium: 2.5,
		SensitivityLow:    3.0,
		"":                2.5,
		"bogus":           2.5,
	}
	for in, want := range cases {
		if got := ZThresholdFor(in); got != want {
			t.Fatalf("ZThresholdFor(%q)=%f want %f", in, got, want)
		}
	}
}
