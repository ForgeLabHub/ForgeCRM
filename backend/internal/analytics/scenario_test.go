package analytics

import (
	"errors"
	"testing"
)

// funnelBaseline builds a two-stage pipeline with round numbers so the expected
// results below can be checked by hand.
//
//	Stage A: 10 open deals, $10,000.00 total, 20% probability
//	Stage B:  5 open deals, $20,000.00 total, 50% probability
//
// Baseline expected won count  = 10*0.20 + 5*0.50 = 2 + 2.5 -> round = 2 + 3 = 5
// Baseline expected won value  = 10000*0.20 + 20000*0.50 = 2000 + 10000 = 12000
//
//	(10_000_00 minor * 2000/10000 = 2_000_00; 20_000_00 * 5000/10000 = 10_000_00)
func funnelBaseline(t *testing.T) Baseline {
	t.Helper()
	return Baseline{
		Currency: "USD",
		Stages: []StageSnapshot{
			{StageID: "a", StageName: "Qualify", Position: 0, OpenCount: 10,
				OpenValue: NewMoney(1_000_000, "USD"), Probability: mustFactor(t, "0.20")},
			{StageID: "b", StageName: "Propose", Position: 1, OpenCount: 5,
				OpenValue: NewMoney(2_000_000, "USD"), Probability: mustFactor(t, "0.50")},
		},
	}
}

func metricByKey(t *testing.T, c *Comparison, key string) Metric {
	t.Helper()
	for _, m := range c.Metrics {
		if m.Key == key {
			return m
		}
	}
	t.Fatalf("metric %q not found in %+v", key, c.Metrics)
	return Metric{}
}

func TestFunnelBaselineIsObserved(t *testing.T) {
	// With no parameter change, baseline and variant must be equal, so the delta
	// is zero. This is the identity a user expects from "no change".
	cmp, err := RunFunnel(funnelBaseline(t), FunnelParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range cmp.Metrics {
		if m.Delta != 0 {
			t.Errorf("metric %s changed with no parameters: base=%d variant=%d",
				m.Key, m.Baseline, m.Variant)
		}
	}
	if got := metricByKey(t, cmp, "expected_won_value").Baseline; got != 1_200_000 {
		t.Errorf("baseline expected won value = %d, want 1200000", got)
	}
	if got := metricByKey(t, cmp, "expected_won_count").Baseline; got != 5 {
		t.Errorf("baseline expected won count = %d, want 5", got)
	}
}

func TestFunnelLeadVolumeScalesEverything(t *testing.T) {
	// A 50% increase in lead volume: open counts 10->15, 5->8 (7.5 rounds to 8),
	// open value 1_000_000->1_500_000 and 2_000_000->3_000_000.
	cmp, err := RunFunnel(funnelBaseline(t), FunnelParams{LeadVolume: mustFactor(t, "1.5")})
	if err != nil {
		t.Fatal(err)
	}
	if got := metricByKey(t, cmp, "pipeline_open_count").Variant; got != 23 {
		t.Errorf("open count variant = %d, want 23 (15 + 8)", got)
	}
	if got := metricByKey(t, cmp, "pipeline_open_value").Variant; got != 4_500_000 {
		t.Errorf("open value variant = %d, want 4500000", got)
	}
	// Expected won value scales linearly with lead volume when the probabilities
	// are unchanged: 1_200_000 * 1.5 = 1_800_000.
	if got := metricByKey(t, cmp, "expected_won_value").Variant; got != 1_800_000 {
		t.Errorf("expected won value variant = %d, want 1800000", got)
	}
}

func TestFunnelStageConversion(t *testing.T) {
	// Doubling stage A's conversion doubles its contribution to the expected won
	// value: 2000 -> 4000, so the total goes 12000 -> 14000.
	cmp, err := RunFunnel(funnelBaseline(t), FunnelParams{
		StageConversions: map[string]Ratio{"a": mustFactor(t, "2.0")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := metricByKey(t, cmp, "expected_won_value").Variant; got != 1_400_000 {
		t.Errorf("expected won value variant = %d, want 1400000", got)
	}
}

// TestFunnelConversionIsClampedToOne proves a conversion above 100% cannot make
// the win probability exceed certainty.
func TestFunnelConversionIsClampedToOne(t *testing.T) {
	cmp, err := RunFunnel(funnelBaseline(t), FunnelParams{
		StageConversions: map[string]Ratio{"a": mustFactor(t, "10.0")},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Stage A is clamped to 100%: 10 * 1.0 = 10 deals, value 10000; plus stage B
	// 10000 -> total 20000.
	if got := metricByKey(t, cmp, "expected_won_value").Variant; got != 2_000_000 {
		t.Errorf("expected won value with clamped conversion = %d, want 2000000", got)
	}
}

func TestFunnelUnknownStageIsWarnedNotIgnored(t *testing.T) {
	cmp, err := RunFunnel(funnelBaseline(t), FunnelParams{
		StageConversions: map[string]Ratio{"does-not-exist": mustFactor(t, "1.5")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Warnings) == 0 {
		t.Fatal("a conversion for an unknown stage produced no warning")
	}
}

func TestFunnelEmptyPipelineIsInsufficientData(t *testing.T) {
	_, err := RunFunnel(Baseline{Currency: "USD"}, FunnelParams{})
	if !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("empty pipeline error = %v, want ErrInsufficientData", err)
	}
}

func TestFunnelRejectsOutOfRangeParameters(t *testing.T) {
	_, err := RunFunnel(funnelBaseline(t), FunnelParams{
		LeadVolume: NewRatioFromBasisPoints(MaxRatioBasisPoints + 1),
	})
	if !errors.Is(err, ErrInvalidParameter) {
		t.Fatalf("out-of-range lead volume error = %v, want ErrInvalidParameter", err)
	}
}

func TestRevenueScalesExactly(t *testing.T) {
	// Won value $100,000.00, 8 won, 2 lost.
	b := Baseline{
		Currency: "USD",
		WonCount: 8, LostCount: 2,
		WonValue: NewMoney(10_000_000, "USD"),
	}
	// +10% ticket, +25% win rate, +20% deal count:
	//   value: 10_000_000 * 1.2 * 1.25 * 1.1 = 16_500_000
	//   count: 8 * 1.2 = 10 (9.6 -> 10); 10 * 1.25 = 13 (12.5 -> 13)
	cmp, err := RunRevenue(b, RevenueParams{
		AvgTicket: mustFactor(t, "1.1"),
		WinRate:   mustFactor(t, "1.25"),
		DealCount: mustFactor(t, "1.2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := metricByKey(t, cmp, "expected_won_value").Variant; got != 16_500_000 {
		t.Errorf("variant revenue = %d, want 16500000", got)
	}
	if got := metricByKey(t, cmp, "expected_won_count").Variant; got != 13 {
		t.Errorf("variant won count = %d, want 13", got)
	}
}

func TestRevenueInsufficientWithoutHistory(t *testing.T) {
	_, err := RunRevenue(Baseline{Currency: "USD"}, RevenueParams{})
	if !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("error = %v, want ErrInsufficientData", err)
	}
}

func TestRevenueNoChangeIsIdentity(t *testing.T) {
	b := Baseline{Currency: "USD", WonCount: 8, LostCount: 2, WonValue: NewMoney(10_000_000, "USD")}
	cmp, err := RunRevenue(b, RevenueParams{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range cmp.Metrics {
		if m.Delta != 0 {
			t.Errorf("metric %s changed with no parameters: base=%d variant=%d", m.Key, m.Baseline, m.Variant)
		}
	}
}

func TestRetentionParametricModel(t *testing.T) {
	// 100 active customers, 20 churned in the period -> observed churn 20%.
	// Halving churn (multiplier 0.5) -> 10%.
	// Customers lost: baseline 20, variant 10. Retained delta = 10.
	// Average deal value $500.00 -> impact = 10 * 500 = $5,000.00 = 500_000 minor.
	b := Baseline{
		Currency:         "USD",
		ActiveCustomers:  100,
		ChurnedCustomers: 20,
		AvgDealValue:     NewMoney(50_000, "USD"),
	}
	cmp, err := RunRetention(b, RetentionParams{ChurnMultiplier: mustFactor(t, "0.5")})
	if err != nil {
		t.Fatal(err)
	}
	if got := metricByKey(t, cmp, "churn_rate").Baseline; got != 2000 {
		t.Errorf("baseline churn rate = %d bp, want 2000", got)
	}
	if got := metricByKey(t, cmp, "churn_rate").Variant; got != 1000 {
		t.Errorf("variant churn rate = %d bp, want 1000", got)
	}
	if got := metricByKey(t, cmp, "customers_retained").Variant; got != 10 {
		t.Errorf("customers retained = %d, want 10", got)
	}
	if got := metricByKey(t, cmp, "financial_impact").Variant; got != 500_000 {
		t.Errorf("financial impact = %d, want 500000", got)
	}
}

func TestRetentionChurnClampedToOne(t *testing.T) {
	b := Baseline{Currency: "USD", ActiveCustomers: 100, ChurnedCustomers: 20, AvgDealValue: NewMoney(50_000, "USD")}
	// A multiplier of 10 pushes churn to 200%, which must clamp to 100%.
	cmp, err := RunRetention(b, RetentionParams{ChurnMultiplier: mustFactor(t, "10")})
	if err != nil {
		t.Fatal(err)
	}
	if got := metricByKey(t, cmp, "churn_rate").Variant; got != 10000 {
		t.Errorf("clamped churn rate = %d bp, want 10000", got)
	}
}

func TestRetentionInsufficientWithoutCustomers(t *testing.T) {
	_, err := RunRetention(Baseline{Currency: "USD"}, RetentionParams{})
	if !errors.Is(err, ErrInsufficientData) {
		t.Fatalf("error = %v, want ErrInsufficientData", err)
	}
}

func TestRetentionZeroCustomerValueWarns(t *testing.T) {
	// No average deal value and no override: the model still runs but warns that
	// the monetary effect is zero, rather than presenting a misleading $0.
	b := Baseline{Currency: "USD", ActiveCustomers: 100, ChurnedCustomers: 20}
	cmp, err := RunRetention(b, RetentionParams{ChurnMultiplier: mustFactor(t, "0.5")})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmp.Warnings) == 0 {
		t.Error("a zero customer value produced no warning")
	}
}

// TestDeterminism is the reproducibility guarantee: the same inputs and
// parameters must produce byte-identical results.
func TestDeterminism(t *testing.T) {
	b := funnelBaseline(t)
	p := FunnelParams{LeadVolume: mustFactor(t, "1.3"), StageConversions: map[string]Ratio{"a": mustFactor(t, "1.1")}}
	first, err := RunFunnel(b, p)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		next, err := RunFunnel(b, p)
		if err != nil {
			t.Fatal(err)
		}
		if len(next.Metrics) != len(first.Metrics) {
			t.Fatalf("run %d produced a different metric count", i)
		}
		for j := range first.Metrics {
			if next.Metrics[j] != first.Metrics[j] {
				t.Fatalf("run %d metric %d differs: %+v vs %+v", i, j, next.Metrics[j], first.Metrics[j])
			}
		}
	}
}

func TestEveryComparisonCarriesVersionAndAssumptions(t *testing.T) {
	runs := []*Comparison{}
	f, err := RunFunnel(funnelBaseline(t), FunnelParams{})
	if err != nil {
		t.Fatal(err)
	}
	runs = append(runs, f)
	r, err := RunRevenue(Baseline{Currency: "USD", WonCount: 1, WonValue: NewMoney(100, "USD")}, RevenueParams{})
	if err != nil {
		t.Fatal(err)
	}
	runs = append(runs, r)
	t2, err := RunRetention(Baseline{Currency: "USD", ActiveCustomers: 10, ChurnedCustomers: 1}, RetentionParams{})
	if err != nil {
		t.Fatal(err)
	}
	runs = append(runs, t2)
	for _, c := range runs {
		if c.AlgorithmVersion != AlgorithmVersion {
			t.Errorf("%s result has version %q, want %q", c.Kind, c.AlgorithmVersion, AlgorithmVersion)
		}
		if len(c.Assumptions) == 0 {
			t.Errorf("%s result states no assumptions", c.Kind)
		}
	}
}
