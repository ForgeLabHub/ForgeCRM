package analytics

import (
	"encoding/json"
	"testing"
)

// The tests here pin the exact arithmetic. Money and ratios are fixed-point, so
// every expected value below is a hand-computed integer, not a tolerance.

func TestParseMoneyExact(t *testing.T) {
	cases := []struct {
		in       string
		currency string
		want     int64
	}{
		{"0", "USD", 0},
		{"1", "USD", 100},
		{"12.34", "USD", 1234},
		{"12.3", "USD", 1230},
		{"-5", "USD", -500},
		{"+5.05", "EUR", 505},
		{"0.01", "USD", 1},
		{"1000000.99", "USD", 100000099},
	}
	for _, c := range cases {
		got, err := ParseMoney(c.in, c.currency)
		if err != nil {
			t.Fatalf("ParseMoney(%q): %v", c.in, err)
		}
		if got.AmountMinor != c.want {
			t.Errorf("ParseMoney(%q) = %d, want %d", c.in, got.AmountMinor, c.want)
		}
		if got.Currency != c.currency {
			t.Errorf("ParseMoney(%q) currency = %q, want %q", c.in, got.Currency, c.currency)
		}
	}
}

func TestParseMoneyRejectsBadInput(t *testing.T) {
	for _, in := range []string{"", "abc", "1.234", "1.2.3", "1,000", "1.", "-", "1e5"} {
		if _, err := ParseMoney(in, "USD"); err == nil {
			t.Errorf("ParseMoney(%q) accepted an invalid amount", in)
		}
	}
}

func TestMoneyStringRoundTrip(t *testing.T) {
	for _, minor := range []int64{0, 1, -1, 99, 100, 1234, -1234, 100000099} {
		m := NewMoney(minor, "USD")
		parsed, err := ParseMoney(m.String(), "USD")
		if err != nil {
			t.Fatalf("re-parse %q: %v", m.String(), err)
		}
		if parsed.AmountMinor != minor {
			t.Errorf("round trip of %d via %q gave %d", minor, m.String(), parsed.AmountMinor)
		}
	}
}

func TestMoneyAddSub(t *testing.T) {
	a := NewMoney(1234, "USD")
	b := NewMoney(8766, "USD")
	sum, err := a.Add(b)
	if err != nil {
		t.Fatal(err)
	}
	if sum.AmountMinor != 10000 {
		t.Errorf("sum = %d, want 10000", sum.AmountMinor)
	}
	diff, err := b.Sub(a)
	if err != nil {
		t.Fatal(err)
	}
	if diff.AmountMinor != 7532 {
		t.Errorf("diff = %d, want 7532", diff.AmountMinor)
	}
}

// TestMoneyAddRefusesMixedCurrency is the property that matters: a total across
// currencies must be refused, not silently summed.
func TestMoneyAddRefusesMixedCurrency(t *testing.T) {
	if _, err := NewMoney(100, "USD").Add(NewMoney(100, "EUR")); err == nil {
		t.Fatal("adding USD and EUR succeeded; it must be refused")
	}
}

func TestRatioPercentSemantics(t *testing.T) {
	// +15% means a factor of 1.15, so 11500 basis points.
	r, err := NewRatioFromPercent("15")
	if err != nil {
		t.Fatal(err)
	}
	if r.BasisPoints != 11500 {
		t.Errorf("+15%% = %d bp, want 11500 (a 15%% increase is a factor of 1.15)", r.BasisPoints)
	}
	// -10% means 0.90.
	r, err = NewRatioFromPercent("-10")
	if err != nil {
		t.Fatal(err)
	}
	if r.BasisPoints != 9000 {
		t.Errorf("-10%% = %d bp, want 9000", r.BasisPoints)
	}
	// A factor of 1.15 is 11500 basis points.
	r, err = NewRatioFromFactor("1.15")
	if err != nil {
		t.Fatal(err)
	}
	if r.BasisPoints != 11500 {
		t.Errorf("factor 1.15 = %d bp, want 11500", r.BasisPoints)
	}
	// +2.5% is 10250 bp.
	r, err = NewRatioFromPercent("2.5")
	if err != nil {
		t.Fatal(err)
	}
	if r.BasisPoints != 10250 {
		t.Errorf("+2.5%% = %d bp, want 10250", r.BasisPoints)
	}
}

func TestMoneyMulRatioRoundsHalfAwayFromZero(t *testing.T) {
	// 1.005 * 1.0 is 100.5 minor units, which must round to 101.
	half := NewMoney(1005, "USD")
	got, err := half.MulRatio(NewRatioFromBasisPoints(1000)) // 0.1
	if err != nil {
		t.Fatal(err)
	}
	// 1005 * 1000 / 10000 = 100.5 -> 101
	if got.AmountMinor != 101 {
		t.Errorf("1005 * 0.1 = %d, want 101 (round half away from zero)", got.AmountMinor)
	}
	// Negative side must round away from zero too: -100.5 -> -101.
	neg := NewMoney(-1005, "USD")
	got, err = neg.MulRatio(NewRatioFromBasisPoints(1000))
	if err != nil {
		t.Fatal(err)
	}
	if got.AmountMinor != -101 {
		t.Errorf("-1005 * 0.1 = %d, want -101", got.AmountMinor)
	}
}

func TestMoneyMulRatioExact(t *testing.T) {
	// $100.00 * 1.15 = $115.00 exactly.
	got, err := NewMoney(10000, "USD").MulRatio(mustFactor(t, "1.15"))
	if err != nil {
		t.Fatal(err)
	}
	if got.AmountMinor != 11500 {
		t.Errorf("10000 * 1.15 = %d, want 11500", got.AmountMinor)
	}
}

func TestRatioApplyCount(t *testing.T) {
	// 10 deals * 1.5 = 15.
	if got := mustFactor(t, "1.5").Apply(10); got != 15 {
		t.Errorf("10 * 1.5 = %d, want 15", got)
	}
	// 3 deals * 1.5 = 4.5 -> 5 (half away from zero).
	if got := mustFactor(t, "1.5").Apply(3); got != 5 {
		t.Errorf("3 * 1.5 = %d, want 5", got)
	}
}

func TestMoneyJSONRoundTrip(t *testing.T) {
	m := NewMoney(123456, "EUR")
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back Money
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
	if back.AmountMinor != m.AmountMinor || back.Currency != m.Currency {
		t.Errorf("round trip changed the value: %+v vs %+v", back, m)
	}
	// The display string must be present and correct.
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if raw["display"] != "1234.56 EUR" {
		t.Errorf("display = %v, want %q", raw["display"], "1234.56 EUR")
	}
}

func TestRatioJSONRoundTrip(t *testing.T) {
	r := mustFactor(t, "1.15")
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back Ratio
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.BasisPoints != r.BasisPoints {
		t.Errorf("ratio round trip changed %d to %d", r.BasisPoints, back.BasisPoints)
	}
}

// mustFactor parses a factor string or fails the test.
func mustFactor(t *testing.T, s string) Ratio {
	t.Helper()
	r, err := NewRatioFromFactor(s)
	if err != nil {
		t.Fatalf("NewRatioFromFactor(%q): %v", s, err)
	}
	return r
}

func TestPercentString(t *testing.T) {
	cases := map[int64]string{
		0:     "0.00%",
		1500:  "15.00%",
		-1000: "-10.00%",
		10250: "102.50%",
		250:   "2.50%",
	}
	for bp, want := range cases {
		if got := PercentString(bp); got != want {
			t.Errorf("PercentString(%d) = %q, want %q", bp, got, want)
		}
	}
}
