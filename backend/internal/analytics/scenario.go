package analytics

import (
	"errors"
	"fmt"
	"math"
)

// AlgorithmVersion identifies the calculation logic. It is stored with every run
// so a result computed by an older version stays interpretable: when the maths
// changes, the version changes, and two runs that differ can be told apart by the
// version rather than guessed at.
const AlgorithmVersion = "simulate-v1"

// Kind identifies a scenario type.
type Kind string

const (
	// KindFunnel models the sales pipeline: how many deals are expected to close
	// and what they are worth, under changed stage conversions or deal inflow.
	KindFunnel Kind = "funnel"
	// KindRevenue models revenue under changed average ticket, win rate or deal
	// volume.
	KindRevenue Kind = "revenue"
	// KindRetention models the effect of a changed customer churn rate.
	KindRetention Kind = "retention"
)

// Valid reports whether the kind is one this package can compute.
func (k Kind) Valid() bool {
	switch k {
	case KindFunnel, KindRevenue, KindRetention:
		return true
	default:
		return false
	}
}

// Unit describes what a metric's integer value means.
type Unit string

const (
	// UnitMoney values are minor units of the metric's currency.
	UnitMoney Unit = "money"
	// UnitCount values are whole counts.
	UnitCount Unit = "count"
	// UnitPercent values are basis points (10000 = 100%).
	UnitPercent Unit = "percent"
)

// Limits bound a computation so a caller cannot ask for an unbounded amount of
// work or a nonsensical parameter.
const (
	// MaxStages caps the funnel stages a single computation walks.
	MaxStages = 64
	// MaxRatioBasisPoints caps a factor at 1000x. A multiplier beyond that is a
	// typo, not a scenario.
	MaxRatioBasisPoints = 1000 * ratioScale
	// MinRatioBasisPoints is the negative floor (-100%).
	MinRatioBasisPoints = -ratioScale
)

// StageSnapshot is one stage of the pipeline as observed, in pipeline order.
type StageSnapshot struct {
	StageID     string
	StageName   string
	Position    int
	OpenCount   int64
	OpenValue   Money
	Probability Ratio // the stage's win probability as a factor (0.0..1.0)
}

// Baseline is the observed CRM data a scenario is computed against. It is read
// once, from the caller's own organization and within their permissions, and is
// frozen into the run record so the result is reproducible.
type Baseline struct {
	Currency string
	// Stages is the funnel, in pipeline order. Used by the funnel scenario.
	Stages []StageSnapshot

	// Won/lost/open history over the selected period. Used by every scenario.
	WonCount     int64
	LostCount    int64
	OpenCount    int64
	WonValue     Money
	LostValue    Money
	OpenValue    Money
	AvgDealValue Money

	// Customer counts for retention. ActiveCustomers is the number of distinct
	// accounts with at least one open or won deal; ChurnedCustomers is the number
	// that had a deal but lost all activity in the period (derived by the reader).
	ActiveCustomers  int64
	ChurnedCustomers int64

	// PeriodDays is the length of the observed window, used to state assumptions.
	PeriodDays int
}

// hasFunnel reports whether there is enough pipeline data to walk the funnel.
func (b Baseline) hasFunnel() bool { return len(b.Stages) > 0 }

// hasHistory reports whether there is any closed-deal history at all.
func (b Baseline) hasHistory() bool { return b.WonCount > 0 || b.LostCount > 0 }

// FunnelParams configures the funnel scenario.
type FunnelParams struct {
	// LeadVolume scales the number of deals entering the funnel. 1.0 leaves it
	// unchanged.
	LeadVolume Ratio
	// StageConversions scales the win probability of named stages. A missing stage
	// keeps its observed probability. A stage named here but absent from the
	// pipeline is reported as a warning, not silently ignored.
	StageConversions map[string]Ratio
}

// RevenueParams configures the revenue scenario.
type RevenueParams struct {
	// AvgTicket scales the average deal value.
	AvgTicket Ratio
	// WinRate scales the win rate.
	WinRate Ratio
	// DealCount scales the number of deals in play.
	DealCount Ratio
}

// RetentionParams configures the retention scenario.
type RetentionParams struct {
	// ChurnMultiplier scales the observed churn rate. 0.8 means churn falls by 20%.
	ChurnMultiplier Ratio
	// CustomerValue optionally overrides the average customer value used for the
	// financial impact. When nil, the value is derived from the baseline won value
	// divided by won count.
	CustomerValue *Money
}

// Metric is one comparable output of a scenario: the same quantity under the
// baseline and the variant.
type Metric struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Unit     Unit   `json:"unit"`
	Currency string `json:"currency,omitempty"`
	// Baseline and Variant are in the metric's unit: minor units for money,
	// a whole number for count, basis points for percent.
	Baseline int64 `json:"baseline"`
	Variant  int64 `json:"variant"`
	// Delta is Variant - Baseline, in the same unit.
	Delta int64 `json:"delta"`
	// RelativeBasisPoints is Delta/Baseline in basis points, when Baseline is
	// non-zero and the metric is not itself a percent. When Baseline is zero the
	// field is omitted (zero value) and RelativeDefined is false: a change from
	// nothing has no meaningful percentage.
	RelativeBasisPoints int64 `json:"relative_basis_points,omitempty"`
	RelativeDefined     bool  `json:"relative_defined"`
}

// Comparison is the full result of a scenario computation.
type Comparison struct {
	Kind             Kind     `json:"kind"`
	AlgorithmVersion string   `json:"algorithm_version"`
	Currency         string   `json:"currency"`
	Metrics          []Metric `json:"metrics"`
	// Assumptions are the stated modelling assumptions a reader must accept for
	// the result to mean anything. They are shown in the UI next to the numbers.
	Assumptions []string `json:"assumptions"`
	// Warnings are inputs that were accepted but are worth flagging, for example a
	// stage conversion named for a stage that does not exist.
	Warnings []string `json:"warnings"`
}

// ErrInsufficientData is returned when the baseline has too little history for a
// scenario to produce a meaningful number. It is a first-class result, not a
// failure: the caller reports "not enough data" rather than a fabricated figure.
var ErrInsufficientData = errors.New("insufficient data for this scenario")

// ErrInvalidParameter is returned for a parameter outside its permitted range.
var ErrInvalidParameter = errors.New("invalid scenario parameter")

// RunFunnel computes the funnel scenario.
func RunFunnel(b Baseline, p FunnelParams) (*Comparison, error) {
	if !b.hasFunnel() {
		return nil, fmt.Errorf("funnel: %w: no pipeline stages", ErrInsufficientData)
	}
	if len(b.Stages) > MaxStages {
		return nil, fmt.Errorf("funnel: %w: %d stages exceeds the %d limit",
			ErrInvalidParameter, len(b.Stages), MaxStages)
	}
	lead := orOne(p.LeadVolume)
	if err := checkRatio("lead volume", lead); err != nil {
		return nil, err
	}
	for stageID, r := range p.StageConversions {
		if err := checkRatio("stage conversion for "+stageID, r); err != nil {
			return nil, err
		}
	}

	cmp := &Comparison{
		Kind:             KindFunnel,
		AlgorithmVersion: AlgorithmVersion,
		Currency:         b.Currency,
		Assumptions: []string{
			"A deal's win probability is the pipeline stage probability observed for that stage; " +
				"the model does not learn a probability from history.",
			"Expected values are a deterministic walk of stage count × stage probability × stage value, " +
				"not a statistical forecast.",
			"Changing lead volume scales every stage's open count and value by the same factor.",
		},
	}

	seen := map[string]bool{}
	var baseOpenCount, varOpenCount int64
	var baseOpenValue, varOpenValue Money
	var err error

	for _, s := range b.Stages {
		if s.Probability.BasisPoints < 0 || s.Probability.BasisPoints > ratioScale {
			return nil, fmt.Errorf("funnel: %w: stage %q has a probability outside 0..1",
				ErrInvalidParameter, s.StageName)
		}
		if _, ok := p.StageConversions[s.StageID]; ok {
			seen[s.StageID] = true
		}

		varValueStage, e := s.OpenValue.MulRatio(lead)
		if e != nil {
			return nil, fmt.Errorf("funnel: scale stage %q value: %w", s.StageName, e)
		}
		baseOpenCount += s.OpenCount
		varOpenCount += lead.Apply(s.OpenCount)
		if baseOpenValue, err = baseOpenValue.Add(s.OpenValue); err != nil {
			return nil, err
		}
		if varOpenValue, err = varOpenValue.Add(varValueStage); err != nil {
			return nil, err
		}
	}

	// Expected won figures apply each stage's probability. The raw open totals are
	// the pipeline size; the expected figures are the point of the scenario, so both
	// are reported.
	baseExpectedCount, baseExpectedValue, err := expectedWon(b.Stages, lead, p.StageConversions, false)
	if err != nil {
		return nil, err
	}
	varExpectedCount, varExpectedValue, err := expectedWon(b.Stages, lead, p.StageConversions, true)
	if err != nil {
		return nil, err
	}

	cmp.Metrics = []Metric{
		newMetric("expected_won_count", "Expected deals won", UnitCount, "", baseExpectedCount, varExpectedCount),
		newMetric("expected_won_value", "Expected won value", UnitMoney, b.Currency,
			baseExpectedValue.AmountMinor, varExpectedValue.AmountMinor),
		newMetric("pipeline_open_count", "Open deals", UnitCount, "", baseOpenCount, varOpenCount),
		newMetric("pipeline_open_value", "Open pipeline value", UnitMoney, b.Currency,
			baseOpenValue.AmountMinor, varOpenValue.AmountMinor),
	}

	// A conversion named for a stage that does not exist is reported rather than
	// dropped: the caller believed it would have an effect.
	for stageID := range p.StageConversions {
		if !seen[stageID] {
			cmp.Warnings = append(cmp.Warnings,
				fmt.Sprintf("conversion for unknown stage %q had no effect", stageID))
		}
	}
	return cmp, nil
}

// expectedWon walks the pipeline and returns the probability-weighted won count
// and value. When variant is true, the lead-volume and conversion parameters are
// applied; when false, the observed baseline is returned unchanged.
func expectedWon(stages []StageSnapshot, lead Ratio, conv map[string]Ratio, variant bool) (int64, Money, error) {
	var count int64
	var value Money
	for _, s := range stages {
		prob := s.Probability
		cnt := s.OpenCount
		val := s.OpenValue
		if variant {
			if c, ok := conv[s.StageID]; ok {
				prob = prob.Mul(c)
			}
			cnt = lead.Apply(cnt)
			scaled, err := val.MulRatio(lead)
			if err != nil {
				return 0, Money{}, err
			}
			val = scaled
		}
		if prob.BasisPoints < 0 {
			prob = Ratio{}
		}
		if prob.BasisPoints > ratioScale {
			prob = OneRatio
		}
		count += prob.Apply(cnt)
		won, err := val.MulRatio(prob)
		if err != nil {
			return 0, Money{}, err
		}
		sum, err := value.Add(won)
		if err != nil {
			return 0, Money{}, err
		}
		value = sum
	}
	return count, value, nil
}

// RunRevenue computes the revenue scenario.
func RunRevenue(b Baseline, p RevenueParams) (*Comparison, error) {
	if !b.hasHistory() {
		return nil, fmt.Errorf("revenue: %w: no won or lost deals in the period", ErrInsufficientData)
	}
	ticket := orOne(p.AvgTicket)
	winRate := orOne(p.WinRate)
	dealCount := orOne(p.DealCount)
	for name, r := range map[string]Ratio{"average ticket": ticket, "win rate": winRate, "deal count": dealCount} {
		if err := checkRatio(name, r); err != nil {
			return nil, err
		}
	}

	// Baseline revenue is the observed won value. The variant applies the three
	// multipliers in sequence. Each money multiplication rounds once.
	varV, err := b.WonValue.MulRatio(dealCount)
	if err != nil {
		return nil, err
	}
	if varV, err = varV.MulRatio(winRate); err != nil {
		return nil, err
	}
	if varV, err = varV.MulRatio(ticket); err != nil {
		return nil, err
	}

	baseWonCount := b.WonCount
	varWonCount := winRate.Apply(dealCount.Apply(baseWonCount))

	cmp := &Comparison{
		Kind:             KindRevenue,
		AlgorithmVersion: AlgorithmVersion,
		Currency:         b.Currency,
		Assumptions: []string{
			"Baseline revenue is the sum of deals won in the selected period.",
			"The variant applies the deal-count, win-rate and average-ticket multipliers to that baseline; " +
				"it is a deterministic scaling, not a forecast.",
			"Amounts are summed in the deal's own currency; a mixed-currency pipeline sums nominal values.",
		},
		Metrics: []Metric{
			newMetric("expected_won_value", "Expected revenue", UnitMoney, b.Currency,
				b.WonValue.AmountMinor, varV.AmountMinor),
			newMetric("expected_won_count", "Expected deals won", UnitCount, "",
				baseWonCount, varWonCount),
			newMetric("win_rate", "Win rate", UnitPercent, "",
				winRateBasisPoints(b.WonCount, b.LostCount),
				winRateBasisPoints(varWonCount, ratioApply(winRate, b.LostCount))),
		},
	}
	return cmp, nil
}

// RunRetention computes the retention scenario. It is a parametric model with
// explicitly stated assumptions, never a trained churn prediction.
func RunRetention(b Baseline, p RetentionParams) (*Comparison, error) {
	if b.ActiveCustomers <= 0 {
		return nil, fmt.Errorf("retention: %w: no active customers in the period", ErrInsufficientData)
	}
	mult := orOne(p.ChurnMultiplier)
	if err := checkRatio("churn multiplier", mult); err != nil {
		return nil, err
	}

	// Observed churn rate in basis points.
	observedBP := (b.ChurnedCustomers * ratioScale) / b.ActiveCustomers
	variantBP := mult.Apply(observedBP)
	if variantBP < 0 {
		variantBP = 0
	}
	if variantBP > ratioScale {
		variantBP = ratioScale
	}

	// Retained customers: the difference between the customers lost at the
	// observed rate and at the variant rate.
	baseLost := (b.ActiveCustomers * observedBP) / ratioScale
	variantLost := (b.ActiveCustomers * variantBP) / ratioScale
	retainedDelta := baseLost - variantLost

	// Customer value: an override, or the observed average won deal value.
	custValue := b.AvgDealValue
	if p.CustomerValue != nil {
		custValue = *p.CustomerValue
	}
	impact, err := custValue.MulRatio(Ratio{BasisPoints: retainedDelta * ratioScale})
	if err != nil {
		return nil, fmt.Errorf("retention: compute financial impact: %w", err)
	}

	cmp := &Comparison{
		Kind:             KindRetention,
		AlgorithmVersion: AlgorithmVersion,
		Currency:         b.Currency,
		Assumptions: []string{
			"This is a parametric model: the churn rate is observed from the period and the change is a " +
				"multiplier you set. It is not a trained or statistical churn prediction.",
			"Financial impact is retained customers × average customer value. The average customer value " +
				"is the observed average won deal value unless you override it.",
			"The model assumes the churn rate is uniform across customers; it does not model cohorts.",
		},
		Metrics: []Metric{
			newMetric("churn_rate", "Churn rate", UnitPercent, "", observedBP, variantBP),
			newMetric("customers_lost", "Customers lost", UnitCount, "", baseLost, variantLost),
			newMetric("customers_retained", "Customers retained vs baseline", UnitCount, "", 0, retainedDelta),
			newMetric("financial_impact", "Financial impact", UnitMoney, b.Currency, 0, impact.AmountMinor),
		},
	}
	if p.CustomerValue == nil && b.AvgDealValue.IsZero() {
		cmp.Warnings = append(cmp.Warnings,
			"no average deal value was available, so the financial impact is zero; "+
				"set a customer value to see a monetary effect")
	}
	return cmp, nil
}

// orOne returns the ratio, or 1.0 when the zero value was passed, so an unset
// multiplier means "no change" rather than "multiply by zero".
func orOne(r Ratio) Ratio {
	if r.BasisPoints == 0 {
		return OneRatio
	}
	return r
}

func checkRatio(name string, r Ratio) error {
	if r.BasisPoints > MaxRatioBasisPoints || r.BasisPoints < MinRatioBasisPoints {
		return fmt.Errorf("%w: %s is outside the permitted range", ErrInvalidParameter, name)
	}
	return nil
}

func ratioApply(r Ratio, count int64) int64 { return r.Apply(count) }

// winRateBasisPoints computes won/(won+lost) in basis points, guarding a zero
// denominator. A zero denominator yields zero and no relative change is defined.
func winRateBasisPoints(won, lost int64) int64 {
	denom := won + lost
	if denom <= 0 {
		return 0
	}
	return (won * ratioScale) / denom
}

// newMetric builds a metric and its derived delta and relative change.
func newMetric(key, label string, unit Unit, currency string, base, variant int64) Metric {
	m := Metric{
		Key: key, Label: label, Unit: unit, Currency: currency,
		Baseline: base, Variant: variant,
		Delta: variant - base,
	}
	// A relative change is only defined against a non-zero baseline, and is not
	// meaningful for a metric that is itself a percentage.
	if unit != UnitPercent && base != 0 {
		m.RelativeBasisPoints = (m.Delta * ratioScale) / base
		m.RelativeDefined = true
	}
	return m
}

// PercentString renders a basis-point value as a human percentage string.
func PercentString(bp int64) string {
	sign := ""
	v := bp
	if v < 0 {
		sign = "-"
		v = -v
	}
	whole := v / 100
	frac := v % 100
	return fmt.Sprintf("%s%d.%02d%%", sign, whole, frac)
}

// FloatPercent is a convenience for charting only. It must never be used for a
// stored or compared monetary value; it exists so a UI layer can size a bar.
func FloatPercent(bp int64) float64 { return math.Round(float64(bp)) / 100 }
