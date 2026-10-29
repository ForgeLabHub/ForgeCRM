package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// ratioScale is the fixed-point denominator for Ratio: one "whole" is
// ratioScale basis points. 10000 basis points = 1.0.
const ratioScale = 10000

// Ratio is a factor held as integer basis points (1/10000), so a multiplier such
// as "1.15" is exact and every product rounds deterministically. Floats are not
// used anywhere in the money path.
type Ratio struct {
	BasisPoints int64
}

// OneRatio is the multiplicative identity (1.0).
var OneRatio = Ratio{BasisPoints: ratioScale}

// NewRatioFromPercent builds a Ratio from a percentage. "15" means 1.15 (a 15%
// increase); "-10" means 0.90.
func NewRatioFromPercent(pct string) (Ratio, error) {
	return NewRatioFromBasisPointsString(pct, true)
}

// NewRatioFromFactor builds a Ratio from a plain multiplier string, e.g. "1.15".
func NewRatioFromFactor(factor string) (Ratio, error) {
	return NewRatioFromBasisPointsString(factor, false)
}

// NewRatioFromBasisPoints builds a Ratio directly from basis points.
func NewRatioFromBasisPoints(bp int64) Ratio { return Ratio{BasisPoints: bp} }

func NewRatioFromBasisPointsString(s string, percent bool) (Ratio, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Ratio{}, errors.New("empty ratio")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	intPart, fracPart, _ := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if !isAllDigits(intPart) {
		return Ratio{}, fmt.Errorf("invalid ratio %q", s)
	}
	// Four decimal places of a percentage is one basis point of a factor.
	if len(fracPart) > 4 {
		return Ratio{}, fmt.Errorf("ratio %q has more precision than one basis point", s)
	}
	for len(fracPart) < 4 {
		fracPart += "0"
	}
	if !isAllDigits(fracPart) {
		return Ratio{}, fmt.Errorf("invalid ratio %q", s)
	}
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return Ratio{}, fmt.Errorf("ratio %q out of range", s)
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return Ratio{}, fmt.Errorf("invalid ratio %q", s)
	}
	// scaled is the magnitude in basis points: whole.frac * ratioScale.
	scaled := new(big.Int).Add(
		new(big.Int).Mul(big.NewInt(whole), big.NewInt(ratioScale)),
		big.NewInt(frac),
	)
	if neg {
		scaled.Neg(scaled)
	}
	var bp *big.Int
	if percent {
		// A percentage is a *change* relative to 1.0: +15% means a factor of
		// 1.15, -10% means 0.90. So bp = ratioScale + scaled/100.
		delta := divRoundHalfAwayFromZero(scaled, big.NewInt(100))
		bp = new(big.Int).Add(big.NewInt(ratioScale), delta)
	} else {
		bp = scaled
	}
	if !bp.IsInt64() {
		return Ratio{}, fmt.Errorf("ratio %q out of range", s)
	}
	return Ratio{BasisPoints: bp.Int64()}, nil
}

// String renders the ratio as a factor with up to four decimals, e.g. "1.1500".
func (r Ratio) String() string {
	neg := r.BasisPoints < 0
	v := r.BasisPoints
	if neg {
		v = -v
	}
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%04d", sign, v/ratioScale, v%ratioScale)
}

// IsOne reports whether the ratio is exactly 1.0.
func (r Ratio) IsOne() bool { return r.BasisPoints == ratioScale }

// IsZero reports whether the ratio is exactly 0.
func (r Ratio) IsZero() bool { return r.BasisPoints == 0 }

// Apply scales an integer count by the ratio, rounding half away from zero. It is
// used for counts (deal numbers), not money.
func (r Ratio) Apply(count int64) int64 {
	product := new(big.Int).Mul(big.NewInt(count), big.NewInt(r.BasisPoints))
	return divRoundHalfAwayFromZero(product, big.NewInt(ratioScale)).Int64()
}

// Mul multiplies two ratios.
func (r Ratio) Mul(other Ratio) Ratio {
	product := new(big.Int).Mul(big.NewInt(r.BasisPoints), big.NewInt(other.BasisPoints))
	return Ratio{BasisPoints: divRoundHalfAwayFromZero(product, big.NewInt(ratioScale)).Int64()}
}

// MarshalJSON writes the ratio as both basis points (exact) and a display factor.
func (r Ratio) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`{"basis_points":%d,"factor":%q}`, r.BasisPoints, r.String())), nil
}

// UnmarshalJSON reads the basis points back, ignoring the display factor.
func (r *Ratio) UnmarshalJSON(data []byte) error {
	var raw struct {
		BasisPoints int64 `json:"basis_points"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode ratio: %w", err)
	}
	r.BasisPoints = raw.BasisPoints
	return nil
}
