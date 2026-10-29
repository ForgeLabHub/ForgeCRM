package analytics

import (
	"testing"
)

func signalOfType(t *testing.T, signals []RiskSignal, typ RiskSignalType, entityID string) RiskSignal {
	t.Helper()
	for _, s := range signals {
		if s.Type == typ && s.EntityID == entityID {
			return s
		}
	}
	t.Fatalf("no %s signal for entity %q in %+v", typ, entityID, signals)
	return RiskSignal{}
}

func TestDetectStalledDealsEmptyData(t *testing.T) {
	// The property that matters: no observations, no signals. An empty
	// organization must not produce warnings about nothing.
	if got := DetectStalledDeals(nil, RiskThresholds{}); len(got) != 0 {
		t.Errorf("empty data produced %d signals, want 0", len(got))
	}
	if got := DetectOverdueTasks(nil, RiskThresholds{}); len(got) != 0 {
		t.Errorf("empty tasks produced %d signals", len(got))
	}
	if got := DetectAccountsWithoutContact(nil, RiskThresholds{}); len(got) != 0 {
		t.Errorf("empty accounts produced %d signals", len(got))
	}
	if got := DetectAmountSpikes(nil, RiskThresholds{}); len(got) != 0 {
		t.Errorf("empty deals produced %d signals", len(got))
	}
	if got := DetectStageConcentration(nil, RiskThresholds{}); len(got) != 0 {
		t.Errorf("empty deals produced %d signals", len(got))
	}
}

func TestDetectStalledDealFiresPastThreshold(t *testing.T) {
	deals := []DealObservation{
		{ID: "d1", Title: "Quiet", AmountMinor: 500000, Currency: "USD", DaysSinceActivity: 45},
		{ID: "d2", Title: "Active", AmountMinor: 500000, Currency: "USD", DaysSinceActivity: 3},
	}
	got := DetectStalledDeals(deals, RiskThresholds{StalledDealDays: 30})
	if len(got) != 1 {
		t.Fatalf("signals = %d, want 1", len(got))
	}
	s := signalOfType(t, got, RiskStalledDeal, "d1")
	if s.Severity != SeverityWarn {
		t.Errorf("severity = %s, want warn", s.Severity)
	}
	if s.Rule != "stalled_deal" || s.Rationale == "" || len(s.Evidence) == 0 {
		t.Errorf("signal is not explainable: %+v", s)
	}
}

func TestDetectStalledDealEscalatesOnPassedCloseDate(t *testing.T) {
	deals := []DealObservation{
		{ID: "d1", Title: "Quiet", AmountMinor: 100, Currency: "USD",
			DaysSinceActivity: 45, CloseDatePassed: true},
	}
	got := DetectStalledDeals(deals, RiskThresholds{})
	if len(got) != 1 || got[0].Severity != SeverityHigh {
		t.Errorf("a stalled deal past its close date should be high severity, got %+v", got)
	}
}

func TestDetectOverdueTaskEscalates(t *testing.T) {
	tasks := []TaskObservation{
		{ID: "t1", Title: "Slightly late", DaysOverdue: 8},
		{ID: "t2", Title: "Very late", DaysOverdue: 30},
		{ID: "t3", Title: "Fine", DaysOverdue: 1},
	}
	got := DetectOverdueTasks(tasks, RiskThresholds{OverdueTaskDays: 7})
	if len(got) != 2 {
		t.Fatalf("signals = %d, want 2", len(got))
	}
	if s := signalOfType(t, got, RiskOverdueTask, "t1"); s.Severity != SeverityWarn {
		t.Errorf("t1 severity = %s, want warn", s.Severity)
	}
	if s := signalOfType(t, got, RiskOverdueTask, "t2"); s.Severity != SeverityHigh {
		t.Errorf("t2 severity = %s, want high", s.Severity)
	}
}

func TestDetectAccountsWithoutContact(t *testing.T) {
	accounts := []AccountObservation{
		{ID: "a1", Name: "No contact", ContactCount: 0, AgeDays: 90},
		{ID: "a2", Name: "Has contact", ContactCount: 2, AgeDays: 90},
		{ID: "a3", Name: "New", ContactCount: 0, AgeDays: 5},
	}
	got := DetectAccountsWithoutContact(accounts, RiskThresholds{AccountNoContactDays: 60})
	if len(got) != 1 {
		t.Fatalf("signals = %d, want 1", len(got))
	}
	if got[0].EntityID != "a1" {
		t.Errorf("signal entity = %s, want a1", got[0].EntityID)
	}
}

func TestDetectAmountSpikeAgainstMedian(t *testing.T) {
	// Median of (100,200,300,400,10000) is 300. With a 3x threshold, only the
	// 10000 deal (33x) fires.
	deals := []DealObservation{
		{ID: "d1", AmountMinor: 100, Currency: "USD"},
		{ID: "d2", AmountMinor: 200, Currency: "USD"},
		{ID: "d3", AmountMinor: 300, Currency: "USD"},
		{ID: "d4", AmountMinor: 400, Currency: "USD"},
		{ID: "d5", AmountMinor: 10000, Currency: "USD"},
	}
	got := DetectAmountSpikes(deals, RiskThresholds{AmountSpikeRatio: NewRatioFromBasisPoints(30_000)})
	if len(got) != 1 || got[0].EntityID != "d5" {
		t.Fatalf("spike signals = %+v, want only d5", got)
	}
}

func TestDetectAmountSpikeNeedsPopulation(t *testing.T) {
	// Fewer than three deals: no median is meaningful, so no spike is claimed.
	deals := []DealObservation{
		{ID: "d1", AmountMinor: 100, Currency: "USD"},
		{ID: "d2", AmountMinor: 1000000, Currency: "USD"},
	}
	if got := DetectAmountSpikes(deals, RiskThresholds{}); len(got) != 0 {
		t.Errorf("spike claimed from two deals: %+v", got)
	}
}

func TestDetectConversionDrop(t *testing.T) {
	periods := []ConversionPeriodObservation{
		{Period: "2026-01", Won: 8, Lost: 2}, // 80%
		{Period: "2026-02", Won: 2, Lost: 8}, // 20%, a big fall
	}
	got := DetectConversionDrops(periods, RiskThresholds{ConversionDropRatio: NewRatioFromBasisPoints(5_000)}, 3)
	if len(got) != 1 {
		t.Fatalf("signals = %d, want 1", len(got))
	}
	if got[0].Type != RiskConversionDrop || got[0].Severity != SeverityHigh {
		t.Errorf("signal = %+v", got[0])
	}
}

func TestDetectConversionDropIgnoresSmallSamples(t *testing.T) {
	// Each period has fewer than minClosed deals, so no rate is claimed.
	periods := []ConversionPeriodObservation{
		{Period: "2026-01", Won: 1, Lost: 0},
		{Period: "2026-02", Won: 0, Lost: 1},
	}
	if got := DetectConversionDrops(periods, RiskThresholds{}, 3); len(got) != 0 {
		t.Errorf("drop claimed from tiny samples: %+v", got)
	}
}

func TestDetectStageConcentration(t *testing.T) {
	deals := []DealObservation{
		{ID: "d1", StageID: "s1", StageName: "Negotiation", AmountMinor: 900000},
		{ID: "d2", StageID: "s2", StageName: "Proposal", AmountMinor: 100000},
	}
	got := DetectStageConcentration(deals, RiskThresholds{StageConcentrationRatio: NewRatioFromBasisPoints(7_000)})
	if len(got) != 1 || got[0].EntityID != "s1" {
		t.Fatalf("concentration signals = %+v, want only s1", got)
	}
}

func TestRiskThresholdsValidate(t *testing.T) {
	if err := (RiskThresholds{StalledDealDays: -1}).Validate(); err == nil {
		t.Errorf("negative day threshold should be rejected")
	}
	if err := (RiskThresholds{AmountSpikeRatio: NewRatioFromBasisPoints(-1)}).Validate(); err == nil {
		t.Errorf("negative ratio threshold should be rejected")
	}
	if err := DefaultRiskThresholds().Validate(); err != nil {
		t.Errorf("defaults should be valid: %v", err)
	}
}

func TestSortSignalsMostSevereFirst(t *testing.T) {
	signals := []RiskSignal{
		{Type: RiskAccountWithoutContact, Severity: SeverityInfo, EntityLabel: "c"},
		{Type: RiskStalledDeal, Severity: SeverityCritical, EntityLabel: "a"},
		{Type: RiskStalledDeal, Severity: SeverityWarn, EntityLabel: "b"},
	}
	SortSignals(signals)
	if signals[0].Severity != SeverityCritical {
		t.Errorf("first signal severity = %s, want critical", signals[0].Severity)
	}
	if signals[2].Severity != SeverityInfo {
		t.Errorf("last signal severity = %s, want info", signals[2].Severity)
	}
}

func TestMedianInt64DoesNotMutateInput(t *testing.T) {
	in := []int64{5, 1, 3}
	_ = medianInt64(in)
	if in[0] != 5 || in[1] != 1 || in[2] != 3 {
		t.Errorf("medianInt64 reordered its input: %v", in)
	}
}
