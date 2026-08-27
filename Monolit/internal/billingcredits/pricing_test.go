package billingcredits

import (
	"errors"
	"math"
	"testing"
)

func TestDynamicPolicyUsesPinnedRational(t *testing.T) {
	credits, err := CreditsFromProviderCostNanoUSDWithPolicy(6_500_000, 10, 7, 2)
	if err != nil {
		t.Fatal(err)
	}
	if credits != 2275 {
		t.Fatalf("credits=%d", credits)
	}
	changed, err := CreditsFromProviderCostNanoUSDWithPolicy(6_500_000, 10, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2600 {
		t.Fatalf("changed credits=%d", changed)
	}
}

func TestCreditsFromProviderCostGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		microUSD    int64
		wantCredits int64
	}{
		{name: "zero", microUSD: 0, wantCredits: 0},
		{name: "minimum rounds once", microUSD: 1, wantCredits: 1},
		{name: "gpt example", microUSD: 6_500, wantCredits: 2_275},
		{name: "standard minute", microUSD: 2_500, wantCredits: 875},
		{name: "diarized minute", microUSD: 2_833, wantCredits: 992},
		{name: "identified minute", microUSD: 3_167, wantCredits: 1_109},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := CreditsFromProviderCost(test.microUSD)
			if err != nil {
				t.Fatalf("CreditsFromProviderCost() error = %v", err)
			}
			if got != test.wantCredits {
				t.Fatalf("CreditsFromProviderCost() = %d, want %d", got, test.wantCredits)
			}
		})
	}
}

func TestCreditsFromProviderCostRejectsInvalidAndOverflow(t *testing.T) {
	t.Parallel()

	if _, err := CreditsFromProviderCost(-1); !errors.Is(err, ErrInvalidCost) {
		t.Fatalf("negative cost error = %v", err)
	}
	if _, err := CreditsFromProviderCost(math.MaxInt64); !errors.Is(err, ErrPricingOverflow) {
		t.Fatalf("overflow error = %v", err)
	}
}

func TestCreditsForAudioRoundsAtOperationBoundary(t *testing.T) {
	t.Parallel()

	// Universal-2: $0.15/hour = 150,000,000 nanoUSD/hour.
	got, err := CreditsForAudio(60, 150_000_000)
	if err != nil {
		t.Fatalf("CreditsForAudio() error = %v", err)
	}
	if got != 875 {
		t.Fatalf("CreditsForAudio() = %d, want 875", got)
	}
}
