package teamanalytics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The criteria table adds up per scorecard and derives the mean and the
// variance from sums; they must match what avg and var_samp gave before.
func TestMeanAndVarianceFromSums(t *testing.T) {
	cases := []struct {
		name     string
		scores   []float64
		mean     *float64
		variance float64
	}{
		{name: "no scores", scores: nil, mean: nil, variance: 0},
		{name: "one score has no spread", scores: []float64{75}, mean: ptr(75), variance: 0},
		{name: "equal scores", scores: []float64{50, 50, 50}, mean: ptr(50), variance: 0},
		{name: "spread", scores: []float64{0, 25, 100, 75}, mean: ptr(50), variance: 6250.0 / 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var total, squares float64
			for _, s := range tc.scores {
				total += s
				squares += s * s
			}
			mean, variance := meanAndVariance(len(tc.scores), total, squares)
			if tc.mean == nil {
				require.Nil(t, mean)
			} else {
				require.NotNil(t, mean)
				require.InDelta(t, *tc.mean, *mean, 1e-9)
			}
			require.InDelta(t, tc.variance, variance, 1e-9)
		})
	}
}

func ptr(v float64) *float64 { return &v }
