package analytics

import (
	"fmt"
	"sort"
	"strings"
)

// This file is the risk core. It turns observations the CRM reader gathered into
// explainable signals. Like the rest of the package it is pure: no database, no
// clock, no model. Every signal carries the rule that fired, the numbers behind
// it, and a severity — so a signal is never an opaque score a user has to trust.
//
// Two rules govern the design:
//
//  1. No signal on empty data. A detector returns nothing when it has no
//     observations, so an empty organization produces an empty list, not a wall
//     of warnings about nothing.
//  2. A signal states its basis. The Evidence list is the observed values the
//     decision used, and the Rationale is the sentence a reader sees. A signal
//     that cannot name its basis is not emitted.

// RiskAlgorithmVersion identifies the risk logic, stored with every signal so a
// signal raised by an older rule set stays interpretable.
const RiskAlgorithmVersion = "risk-v1"

// RiskSeverity ranks a signal. The order is meaningful: a higher value is more
// urgent, so a caller can sort and filter without a lookup table.
type RiskSeverity string

const (
	SeverityInfo     RiskSeverity = "info"
	SeverityWarn     RiskSeverity = "warn"
	SeverityHigh     RiskSeverity = "high"
	SeverityCritical RiskSeverity = "critical"
)

// Rank returns the severity's order. An unknown severity ranks lowest.
func (s RiskSeverity) Rank() int {
	switch s {
	case SeverityCritical:
		return 4
	case SeverityHigh:
		return 3
	case SeverityWarn:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Valid reports whether the severity is one this package emits.
func (s RiskSeverity) Valid() bool { return s.Rank() > 0 }

// RiskSignalType identifies the rule that produced a signal.
type RiskSignalType string

const (
	// RiskStalledDeal: an open deal with no recorded activity for a while.
	RiskStalledDeal RiskSignalType = "stalled_deal"
	// RiskOverdueTask: a task past its due date and still not done.
	RiskOverdueTask RiskSignalType = "overdue_task"
	// RiskAccountWithoutContact: an account with no contact person at all.
	RiskAccountWithoutContact RiskSignalType = "account_without_contact"
	// RiskAmountSpike: a deal amount far above the organization's typical deal.
	RiskAmountSpike RiskSignalType = "amount_spike"
	// RiskConversionDrop: the win rate fell sharply period over period.
	RiskConversionDrop RiskSignalType = "conversion_drop"
	// RiskStageConcentration: most of the open pipeline value sits in one stage,
	// which is a fragile funnel rather than a healthy one.
	RiskStageConcentration RiskSignalType = "stage_concentration"
)

// Valid reports whether the type is one this package emits.
func (t RiskSignalType) Valid() bool {
	switch t {
	case RiskStalledDeal, RiskOverdueTask, RiskAccountWithoutContact,
		RiskAmountSpike, RiskConversionDrop, RiskStageConcentration:
		return true
	default:
		return false
	}
}

// Evidence is one observed value behind a signal, so the reader can see the basis
// rather than take the signal on faith.
type Evidence struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}

// RiskSignal is one explainable risk.
type RiskSignal struct {
	Type             RiskSignalType `json:"type"`
	Severity         RiskSeverity   `json:"severity"`
	AlgorithmVersion string         `json:"algorithm_version"`
	// EntityType and EntityID name the CRM record the signal is about, so the UI
	// can link to it and the API can re-check access on read.
	EntityType  string `json:"entity_type"`
	EntityID    string `json:"entity_id"`
	EntityLabel string `json:"entity_label"`
	// Rule is the identifier of the rule that fired, for example "stalled_deal".
	Rule string `json:"rule"`
	// Rationale is the human sentence explaining why the signal fired.
	Rationale string `json:"rationale"`
	// Evidence is the observed values the decision used.
	Evidence []Evidence `json:"evidence"`
}

// RiskThresholds are the configurable cutoffs. A zero value means "use the
// default", so a caller can set only the thresholds it cares about. They are not
// aggressive: the defaults flag a deal only after a month of silence, not a week.
type RiskThresholds struct {
	// StalledDealDays: an open deal with no activity for at least this many days.
	StalledDealDays int
	// OverdueTaskDays: a task at least this many days past its due date.
	OverdueTaskDays int
	// AccountNoContactDays: an account at least this many days old with no contact.
	AccountNoContactDays int
	// AmountSpikeRatio: a deal amount at least this multiple of the median deal.
	AmountSpikeRatio Ratio
	// ConversionDropRatio: the win rate fell to at most this fraction of the
	// previous period's, and by at least ConversionDropPoints.
	ConversionDropRatio Ratio
	// StageConcentrationRatio: a stage holding at least this share of open value.
	StageConcentrationRatio Ratio
}

// DefaultRiskThresholds returns the shipped defaults. They are deliberately
// conservative: a signal that fires on healthy data teaches users to ignore it.
func DefaultRiskThresholds() RiskThresholds {
	return RiskThresholds{
		StalledDealDays:         30,
		OverdueTaskDays:         7,
		AccountNoContactDays:    60,
		AmountSpikeRatio:        NewRatioFromBasisPoints(30_000), // 3.0x
		ConversionDropRatio:     NewRatioFromBasisPoints(5_000),  // fell to <= 50%
		StageConcentrationRatio: NewRatioFromBasisPoints(7_000),  // >= 70%
	}
}

// withDefaults fills any unset threshold from the defaults, so a partial config
// is valid and the result is still bounded.
func (t RiskThresholds) withDefaults() RiskThresholds {
	d := DefaultRiskThresholds()
	if t.StalledDealDays <= 0 {
		t.StalledDealDays = d.StalledDealDays
	}
	if t.OverdueTaskDays <= 0 {
		t.OverdueTaskDays = d.OverdueTaskDays
	}
	if t.AccountNoContactDays <= 0 {
		t.AccountNoContactDays = d.AccountNoContactDays
	}
	if t.AmountSpikeRatio.BasisPoints == 0 {
		t.AmountSpikeRatio = d.AmountSpikeRatio
	}
	if t.ConversionDropRatio.BasisPoints == 0 {
		t.ConversionDropRatio = d.ConversionDropRatio
	}
	if t.StageConcentrationRatio.BasisPoints == 0 {
		t.StageConcentrationRatio = d.StageConcentrationRatio
	}
	return t
}

// Validate rejects a threshold set that could produce nonsensical signals.
func (t RiskThresholds) Validate() error {
	if t.StalledDealDays < 0 || t.OverdueTaskDays < 0 || t.AccountNoContactDays < 0 {
		return fmt.Errorf("%w: a day threshold cannot be negative", ErrInvalidParameter)
	}
	if t.AmountSpikeRatio.BasisPoints < 0 || t.ConversionDropRatio.BasisPoints < 0 ||
		t.StageConcentrationRatio.BasisPoints < 0 {
		return fmt.Errorf("%w: a ratio threshold cannot be negative", ErrInvalidParameter)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Observations. The reader gathers these; the detectors below are pure functions
// of them. Ids are strings because this package imports only the standard
// library, and a caller that has uuids formats them.
// ---------------------------------------------------------------------------

// DealObservation is a deal as the risk detectors see it.
type DealObservation struct {
	ID          string
	Title       string
	AmountMinor int64
	Currency    string
	// StageID and StageName are set for open deals, for the concentration rule.
	StageID   string
	StageName string
	// DaysSinceActivity is the age of the most recent activity on the deal, or the
	// age of the deal itself when it has none. The reader computes it, because this
	// package has no clock.
	DaysSinceActivity int
	// CloseDatePassed is true when the expected close date is in the past.
	CloseDatePassed bool
}

// TaskObservation is a task as the risk detectors see it.
type TaskObservation struct {
	ID    string
	Title string
	// DaysOverdue is how far past the due date the task is. A task with no due date
	// or a future one is not included by the reader.
	DaysOverdue int
	Assignee    string
}

// AccountObservation is an account as the risk detectors see it.
type AccountObservation struct {
	ID           string
	Name         string
	ContactCount int
	// AgeDays is how long ago the account was created.
	AgeDays int
}

// ConversionPeriodObservation is one period's win/loss counts, for the trend rule.
type ConversionPeriodObservation struct {
	Period string
	Won    int64
	Lost   int64
}

// DetectStalledDeals raises a signal for each open deal that has gone quiet.
func DetectStalledDeals(deals []DealObservation, th RiskThresholds) []RiskSignal {
	th = th.withDefaults()
	var out []RiskSignal
	for _, d := range deals {
		if d.DaysSinceActivity < th.StalledDealDays {
			continue
		}
		severity := SeverityWarn
		if d.CloseDatePassed {
			severity = SeverityHigh
		}
		rationale := fmt.Sprintf("No recorded activity for %d days (threshold %d).",
			d.DaysSinceActivity, th.StalledDealDays)
		if d.CloseDatePassed {
			rationale += " The expected close date has passed."
		}
		out = append(out, RiskSignal{
			Type: RiskStalledDeal, Severity: severity, AlgorithmVersion: RiskAlgorithmVersion,
			EntityType: "deal", EntityID: d.ID, EntityLabel: d.Title,
			Rule: "stalled_deal", Rationale: rationale,
			Evidence: []Evidence{
				{Key: "days_since_activity", Label: "Days since activity", Value: fmt.Sprintf("%d", d.DaysSinceActivity)},
				{Key: "threshold_days", Label: "Threshold (days)", Value: fmt.Sprintf("%d", th.StalledDealDays)},
				{Key: "amount", Label: "Amount", Value: Money{AmountMinor: d.AmountMinor, Currency: d.Currency}.String()},
				{Key: "close_date_passed", Label: "Close date passed", Value: fmt.Sprintf("%t", d.CloseDatePassed)},
			},
		})
	}
	return out
}

// DetectOverdueTasks raises a signal for each task past its due date.
func DetectOverdueTasks(tasks []TaskObservation, th RiskThresholds) []RiskSignal {
	th = th.withDefaults()
	var out []RiskSignal
	for _, t := range tasks {
		if t.DaysOverdue < th.OverdueTaskDays {
			continue
		}
		severity := SeverityWarn
		if t.DaysOverdue >= th.OverdueTaskDays*3 {
			severity = SeverityHigh
		}
		out = append(out, RiskSignal{
			Type: RiskOverdueTask, Severity: severity, AlgorithmVersion: RiskAlgorithmVersion,
			EntityType: "task", EntityID: t.ID, EntityLabel: t.Title,
			Rule: "overdue_task",
			Rationale: fmt.Sprintf("Overdue by %d days (threshold %d).",
				t.DaysOverdue, th.OverdueTaskDays),
			Evidence: []Evidence{
				{Key: "days_overdue", Label: "Days overdue", Value: fmt.Sprintf("%d", t.DaysOverdue)},
				{Key: "threshold_days", Label: "Threshold (days)", Value: fmt.Sprintf("%d", th.OverdueTaskDays)},
				{Key: "assignee", Label: "Assignee", Value: t.Assignee},
			},
		})
	}
	return out
}

// DetectAccountsWithoutContact raises a signal for each account old enough to be
// expected to have a contact but holding none.
func DetectAccountsWithoutContact(accounts []AccountObservation, th RiskThresholds) []RiskSignal {
	th = th.withDefaults()
	var out []RiskSignal
	for _, a := range accounts {
		if a.ContactCount > 0 || a.AgeDays < th.AccountNoContactDays {
			continue
		}
		out = append(out, RiskSignal{
			Type: RiskAccountWithoutContact, Severity: SeverityInfo, AlgorithmVersion: RiskAlgorithmVersion,
			EntityType: "account", EntityID: a.ID, EntityLabel: a.Name,
			Rule: "account_without_contact",
			Rationale: fmt.Sprintf("Account is %d days old and has no contact person.",
				a.AgeDays),
			Evidence: []Evidence{
				{Key: "contact_count", Label: "Contacts", Value: "0"},
				{Key: "age_days", Label: "Account age (days)", Value: fmt.Sprintf("%d", a.AgeDays)},
				{Key: "threshold_days", Label: "Threshold (days)", Value: fmt.Sprintf("%d", th.AccountNoContactDays)},
			},
		})
	}
	return out
}

// DetectAmountSpikes raises a signal for each deal whose amount is far above the
// organization's median deal. The median is used rather than the mean so one
// existing outlier does not raise the bar for the next one.
func DetectAmountSpikes(deals []DealObservation, th RiskThresholds) []RiskSignal {
	th = th.withDefaults()
	if len(deals) < 3 {
		// Too few deals for a median to mean anything; a spike needs a population.
		return nil
	}
	amounts := make([]int64, 0, len(deals))
	for _, d := range deals {
		amounts = append(amounts, d.AmountMinor)
	}
	median := medianInt64(amounts)
	if median <= 0 {
		return nil
	}
	var out []RiskSignal
	for _, d := range deals {
		if d.AmountMinor <= 0 {
			continue
		}
		// amount >= median * ratio, computed in integers to avoid a float.
		threshold := (median * th.AmountSpikeRatio.BasisPoints) / ratioScale
		if d.AmountMinor < threshold {
			continue
		}
		out = append(out, RiskSignal{
			Type: RiskAmountSpike, Severity: SeverityWarn, AlgorithmVersion: RiskAlgorithmVersion,
			EntityType: "deal", EntityID: d.ID, EntityLabel: d.Title,
			Rule: "amount_spike",
			Rationale: fmt.Sprintf("Amount is %.1fx the median deal for this organization.",
				float64(d.AmountMinor)/float64(median)),
			Evidence: []Evidence{
				{Key: "amount", Label: "Amount", Value: Money{AmountMinor: d.AmountMinor, Currency: d.Currency}.String()},
				{Key: "median_amount", Label: "Median deal", Value: Money{AmountMinor: median, Currency: d.Currency}.String()},
				{Key: "ratio", Label: "Ratio to median", Value: Ratio{BasisPoints: (d.AmountMinor * ratioScale) / median}.String()},
			},
		})
	}
	return out
}

// DetectConversionDrops raises a signal when the win rate fell sharply between
// two consecutive periods. It needs at least two periods, and each must have
// enough closed deals for a rate to be meaningful.
func DetectConversionDrops(periods []ConversionPeriodObservation, th RiskThresholds, minClosed int64) []RiskSignal {
	th = th.withDefaults()
	if len(periods) < 2 {
		return nil
	}
	if minClosed <= 0 {
		minClosed = 3
	}
	var out []RiskSignal
	for i := 1; i < len(periods); i++ {
		prev, cur := periods[i-1], periods[i]
		prevClosed := prev.Won + prev.Lost
		curClosed := cur.Won + cur.Lost
		if prevClosed < minClosed || curClosed < minClosed {
			continue
		}
		prevRate := (prev.Won * ratioScale) / prevClosed
		curRate := (cur.Won * ratioScale) / curClosed
		if prevRate <= 0 {
			continue
		}
		// curRate <= prevRate * dropRatio
		threshold := (prevRate * th.ConversionDropRatio.BasisPoints) / ratioScale
		if curRate > threshold {
			continue
		}
		out = append(out, RiskSignal{
			Type: RiskConversionDrop, Severity: SeverityHigh, AlgorithmVersion: RiskAlgorithmVersion,
			EntityType: "pipeline", EntityID: cur.Period, EntityLabel: "Win rate",
			Rule: "conversion_drop",
			Rationale: fmt.Sprintf("Win rate fell from %s in %s to %s in %s.",
				PercentString(prevRate), prev.Period, PercentString(curRate), cur.Period),
			Evidence: []Evidence{
				{Key: "period", Label: "Period", Value: cur.Period},
				{Key: "previous_win_rate", Label: "Previous win rate", Value: PercentString(prevRate)},
				{Key: "current_win_rate", Label: "Current win rate", Value: PercentString(curRate)},
				{Key: "previous_closed", Label: "Previous closed deals", Value: fmt.Sprintf("%d", prevClosed)},
				{Key: "current_closed", Label: "Current closed deals", Value: fmt.Sprintf("%d", curClosed)},
			},
		})
	}
	return out
}

// DetectStageConcentration raises a signal when a single stage holds most of the
// open pipeline value. A funnel that depends on one stage is fragile, and the
// signal names the stage and the share.
func DetectStageConcentration(deals []DealObservation, th RiskThresholds) []RiskSignal {
	th = th.withDefaults()
	if len(deals) == 0 {
		return nil
	}
	byStage := map[string]int64{}
	nameByStage := map[string]string{}
	var total int64
	for _, d := range deals {
		if d.StageID == "" {
			continue
		}
		byStage[d.StageID] += d.AmountMinor
		nameByStage[d.StageID] = d.StageName
		total += d.AmountMinor
	}
	if total <= 0 {
		return nil
	}
	var out []RiskSignal
	for stageID, sum := range byStage {
		share := (sum * ratioScale) / total
		if share < th.StageConcentrationRatio.BasisPoints {
			continue
		}
		out = append(out, RiskSignal{
			Type: RiskStageConcentration, Severity: SeverityInfo, AlgorithmVersion: RiskAlgorithmVersion,
			EntityType: "pipeline_stage", EntityID: stageID, EntityLabel: nameByStage[stageID],
			Rule: "stage_concentration",
			Rationale: fmt.Sprintf("%s holds %s of the open pipeline value.",
				nameByStage[stageID], PercentString(share)),
			Evidence: []Evidence{
				{Key: "stage_value", Label: "Stage value", Value: Money{AmountMinor: sum}.String()},
				{Key: "pipeline_value", Label: "Open pipeline value", Value: Money{AmountMinor: total}.String()},
				{Key: "share", Label: "Share", Value: PercentString(share)},
			},
		})
	}
	return out
}

// medianInt64 returns the median of the values, rounding the middle pair's mean
// toward zero. It copies its input so the caller's slice is not reordered.
func medianInt64(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	cp := append([]int64(nil), values...)
	sort.Slice(cp, func(i, j int) bool { return cp[i] < cp[j] })
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// SortSignals orders signals most severe first, then by type and entity, so the
// list is stable and a UI can show the urgent ones at the top without its own
// sort. It is stable, so two equally severe signals keep their input order.
func SortSignals(signals []RiskSignal) {
	sort.SliceStable(signals, func(i, j int) bool {
		si, sj := signals[i].Severity.Rank(), signals[j].Severity.Rank()
		if si != sj {
			return si > sj
		}
		if signals[i].Type != signals[j].Type {
			return signals[i].Type < signals[j].Type
		}
		return strings.Compare(signals[i].EntityLabel, signals[j].EntityLabel) < 0
	})
}
