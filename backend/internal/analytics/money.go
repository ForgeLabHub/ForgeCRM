// Package analytics is the pure calculation core of the intelligence platform.
//
// It holds the arithmetic the Simulation Lab and (later) forecasting and risk
// scoring are built on, and nothing else. The package imports only the standard
// library: no database, no HTTP, no AI provider, no clock. That boundary is the
// point. The maths is deterministic, so the same inputs and parameters always
// produce the same result, and it can be reviewed and unit-tested without a
// database or a running service.
//
// Money is never a float. Amounts are held as integer minor units (cents) in a
// Money value, and every operation that could produce a fraction rounds once, at
// the defined boundary, using round-half-away-from-zero. A float is never used
// for an amount, so a stored result cannot drift when it is reloaded.
package analytics

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// moneyScale is the number of minor units in one major unit. Two decimal places
// covers every currency the CRM stores today; a currency with a different
// exponent would carry its own scale, which is a change to make when such a
// currency is actually supported rather than guessed at now.
const moneyScale = 100

// Money is an exact monetary amount: an integer count of minor units (cents) in
// a named currency. The zero value is "0" of the empty currency; use NewMoney to
// name a currency.
type Money struct {
	// AmountMinor is the amount in minor units. For USD, 1234 is $12.34.
	AmountMinor int64
	// Currency is the ISO 4217 code, upper case, three letters.
	Currency string
}

// NewMoney builds a Money value.
func NewMoney(amountMinor int64, currency string) Money {
	return Money{AmountMinor: amountMinor, Currency: normalizeCurrency(currency)}
}

func normalizeCurrency(c string) string {
	c = strings.ToUpper(strings.TrimSpace(c))
	if c == "" {
		return "USD"
	}
	return c
}

// ParseMoney parses a decimal string such as "1234.56" or "-5" into Money.
//
// It rejects more than two decimal places rather than silently truncating: a
// caller that passed three decimals expected them to be kept, and rounding them
// away without saying so is how an amount quietly becomes wrong.
func ParseMoney(s, currency string) (Money, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Money{}, errors.New("empty amount")
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return Money{}, fmt.Errorf("invalid amount %q", s)
	}
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	if intPart == "" {
		intPart = "0"
	}
	if !isAllDigits(intPart) {
		return Money{}, fmt.Errorf("invalid amount %q", s)
	}
	if hasFrac {
		if fracPart == "" || len(fracPart) > 2 {
			return Money{}, fmt.Errorf("amount %q has an invalid decimal part", s)
		}
		if !isAllDigits(fracPart) {
			return Money{}, fmt.Errorf("invalid amount %q", s)
		}
		for len(fracPart) < 2 {
			fracPart += "0"
		}
	} else {
		fracPart = "00"
	}
	major, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("amount %q out of range", s)
	}
	minor, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return Money{}, fmt.Errorf("invalid amount %q", s)
	}
	total := major*moneyScale + minor
	if neg {
		total = -total
	}
	return Money{AmountMinor: total, Currency: normalizeCurrency(currency)}, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// String renders the amount as a decimal string in major units, e.g. "-12.34".
func (m Money) String() string {
	neg := m.AmountMinor < 0
	v := m.AmountMinor
	if neg {
		v = -v
	}
	major := v / moneyScale
	minor := v % moneyScale
	sign := ""
	if neg {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%02d", sign, major, minor)
}

// IsZero reports whether the amount is zero.
func (m Money) IsZero() bool { return m.AmountMinor == 0 }

// sameCurrency reports whether two amounts share a currency, treating the empty
// currency as matching anything so a zero value can be added to a named one.
func (m Money) sameCurrency(other Money) bool {
	return m.Currency == "" || other.Currency == "" || m.Currency == other.Currency
}

// Add returns m + other. Adding across currencies is an error: the CRM sums
// nominal values across currencies in its reports and says so, but the simulation
// core refuses, because a silently mixed total is worse than a stated refusal.
func (m Money) Add(other Money) (Money, error) {
	if !m.sameCurrency(other) {
		return Money{}, fmt.Errorf("cannot add %s and %s", m.Currency, other.Currency)
	}
	cur := m.Currency
	if cur == "" {
		cur = other.Currency
	}
	sum := new(big.Int).Add(big.NewInt(m.AmountMinor), big.NewInt(other.AmountMinor))
	if !sum.IsInt64() {
		return Money{}, errors.New("amount overflow")
	}
	return Money{AmountMinor: sum.Int64(), Currency: cur}, nil
}

// Sub returns m - other.
func (m Money) Sub(other Money) (Money, error) {
	return m.Add(other.Neg())
}

// Neg returns the amount with the opposite sign.
func (m Money) Neg() Money { return Money{AmountMinor: -m.AmountMinor, Currency: m.Currency} }

// Cmp compares two amounts of the same currency: -1, 0 or 1.
func (m Money) Cmp(other Money) int {
	switch {
	case m.AmountMinor < other.AmountMinor:
		return -1
	case m.AmountMinor > other.AmountMinor:
		return 1
	default:
		return 0
	}
}

// MulRatio scales the amount by a Ratio, rounding once at the defined boundary.
// It is the only way a fractional factor enters money arithmetic, so the rounding
// rule lives in one place.
func (m Money) MulRatio(r Ratio) (Money, error) {
	product := new(big.Int).Mul(big.NewInt(m.AmountMinor), big.NewInt(r.BasisPoints))
	rounded := divRoundHalfAwayFromZero(product, big.NewInt(ratioScale))
	if !rounded.IsInt64() {
		return Money{}, errors.New("amount overflow")
	}
	return Money{AmountMinor: rounded.Int64(), Currency: m.Currency}, nil
}

// divRoundHalfAwayFromZero divides n by d, rounding a half away from zero. Both
// inputs are treated as signed; d must be positive.
func divRoundHalfAwayFromZero(n, d *big.Int) *big.Int {
	neg := n.Sign() < 0
	abs := new(big.Int).Abs(n)
	q, rem := new(big.Int).QuoRem(abs, d, new(big.Int))
	twiceRem := new(big.Int).Lsh(rem, 1)
	if twiceRem.Cmp(d) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q
}

// MarshalJSON writes the amount as an object carrying the exact minor units, the
// currency and a display string. The minor units are the source of truth; the
// display string is for humans and is never parsed back for arithmetic.
func (m Money) MarshalJSON() ([]byte, error) {
	display := m.String()
	if m.Currency != "" {
		display = m.String() + " " + m.Currency
	}
	return []byte(fmt.Sprintf(`{"amount_minor":%d,"currency":%q,"display":%q}`,
		m.AmountMinor, m.Currency, display)), nil
}

// moneyJSON is the wire shape of Money.
type moneyJSON struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

// UnmarshalJSON reads the wire shape back, ignoring the display string so a
// round-trip cannot change the value.
func (m *Money) UnmarshalJSON(data []byte) error {
	// The `display` member MarshalJSON writes is intentionally not part of
	// moneyJSON, so decoding tolerates it rather than rejecting the value it just
	// produced. Only the minor units and currency are read back.
	var raw moneyJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode money: %w", err)
	}
	m.AmountMinor = raw.AmountMinor
	m.Currency = normalizeCurrency(raw.Currency)
	return nil
}
