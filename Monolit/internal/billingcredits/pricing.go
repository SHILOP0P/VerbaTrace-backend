package billingcredits

import (
	"errors"
	"math"
	"math/big"
)

const (
	CreditMicroUSD        int64 = 10
	MultiplierNumerator   int64 = 7
	MultiplierDenominator int64 = 2
)

var (
	ErrInvalidCost     = errors.New("provider cost must not be negative")
	ErrInvalidDuration = errors.New("duration must not be negative")
	ErrPricingOverflow = errors.New("credit pricing overflow")
)

// CreditsFromProviderCost converts the provider charge into VerbaTrace credits.
// The operation uses checked integer arithmetic and rounds up exactly once:
// ceil(providerMicroUSD * 7 / (10 * 2)).
func CreditsFromProviderCost(providerMicroUSD int64) (int64, error) {
	if providerMicroUSD < 0 {
		return 0, ErrInvalidCost
	}
	if providerMicroUSD == 0 {
		return 0, nil
	}
	if providerMicroUSD > math.MaxInt64/MultiplierNumerator {
		return 0, ErrPricingOverflow
	}

	numerator := providerMicroUSD * MultiplierNumerator
	denominator := CreditMicroUSD * MultiplierDenominator
	return ceilDiv(numerator, denominator), nil
}

// CreditsFromProviderCostNanoUSD converts an exact nanoUSD provider charge and
// performs the only rounding at the final credit boundary.
func CreditsFromProviderCostNanoUSD(providerNanoUSD int64) (int64, error) {
	if providerNanoUSD < 0 {
		return 0, ErrInvalidCost
	}
	return checkedCeilProduct(
		[]int64{providerNanoUSD, MultiplierNumerator},
		[]int64{1_000, CreditMicroUSD, MultiplierDenominator},
	)
}

func CreditsFromProviderCostNanoUSDWithPolicy(providerNanoUSD, creditMicroUSD, multiplierNumerator, multiplierDenominator int64) (int64, error) {
	if providerNanoUSD < 0 {
		return 0, ErrInvalidCost
	}
	if creditMicroUSD <= 0 || multiplierNumerator <= 0 || multiplierDenominator <= 0 {
		return 0, ErrPricingOverflow
	}
	return checkedCeilProduct([]int64{providerNanoUSD, multiplierNumerator}, []int64{1_000, creditMicroUSD, multiplierDenominator})
}

func CreditsForAudioWithPolicy(durationSeconds, nanoUSDPerHour, creditMicroUSD, multiplierNumerator, multiplierDenominator int64) (int64, error) {
	if durationSeconds < 0 || nanoUSDPerHour < 0 {
		return 0, ErrInvalidDuration
	}
	if durationSeconds == 0 || nanoUSDPerHour == 0 {
		return 0, nil
	}
	return checkedCeilProduct([]int64{durationSeconds, nanoUSDPerHour, multiplierNumerator}, []int64{3_600, 1_000, creditMicroUSD, multiplierDenominator})
}

// CreditsForAudio prices an audio operation from the catalog's exact nanoUSD/hour
// rate and rounds up once for the complete operation.
func CreditsForAudio(durationSeconds, nanoUSDPerHour int64) (int64, error) {
	if durationSeconds < 0 || nanoUSDPerHour < 0 {
		return 0, ErrInvalidDuration
	}
	if durationSeconds == 0 || nanoUSDPerHour == 0 {
		return 0, nil
	}
	return checkedCeilProduct(
		[]int64{durationSeconds, nanoUSDPerHour, MultiplierNumerator},
		[]int64{3_600, 1_000, CreditMicroUSD, MultiplierDenominator},
	)
}

func ceilDiv(numerator, denominator int64) int64 {
	return numerator/denominator + boolInt(numerator%denominator != 0)
}

func boolInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func checkedCeilProduct(numerators, denominators []int64) (int64, error) {
	numerator := big.NewInt(1)
	for _, value := range numerators {
		if value < 0 {
			return 0, ErrInvalidCost
		}
		numerator.Mul(numerator, big.NewInt(value))
	}
	denominator := big.NewInt(1)
	for _, value := range denominators {
		if value <= 0 {
			return 0, ErrPricingOverflow
		}
		denominator.Mul(denominator, big.NewInt(value))
	}

	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)
	if remainder.Sign() != 0 {
		quotient.Add(quotient, big.NewInt(1))
	}
	if !quotient.IsInt64() {
		return 0, ErrPricingOverflow
	}
	return quotient.Int64(), nil
}
