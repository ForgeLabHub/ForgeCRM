package analytics

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
)

// This file is the forecasting core. Like the rest of the package it is pure:
// standard library only, no database, no clock, no model. A forecast is a
// deterministic calculation on an observed series, and every number it returns
// can be recomputed by hand from the inputs.
//
// The design rule that matters most is evaluation. A forecast is only worth
// anything if its error is measured out of sample, so the backtest fits on a
// prefix of the series and scores the next point, then walks forward. It never
// scores a point the fit has already seen, which is the mistake that makes a
// forecast look better than it is. The result is reported next to a naive
// baseline (carry the last value forward): a method that does not beat the naive
// baseline has not earned its complexity, and the caller is told so.

// ForecastAlgorithmVersion identifies the forecasting logic, stored with every
// run so a result computed by an older version stays interpretable.
const ForecastAlgorithmVersion = "forecast-v1"

// ForecastMethod identifies a forecasting method.
type ForecastMethod string

const (
	// MethodMean forecasts every future period as the mean of the observed
	// series. The simplest method that uses all the data.
	MethodMean ForecastMethod = "mean"
	// MethodMovingAverage forecasts every future period as the mean of the last
	// Window observations. Responds to a recent level shift.
	MethodMovingAverage ForecastMethod = "moving_average"
	// MethodTrend fits a least-squares straight line to the series and extends it.
	// Only meaningful when the series is roughly linear; the backtest says whether
	// it was.
	MethodTrend ForecastMethod = "trend"
)

// Valid reports whether the method is one this package computes.
func (m ForecastMethod) Valid() bool {
	switch m {
	case MethodMean, MethodMovingAverage, MethodTrend:
		return true
	default:
		return false
	}
}

// Forecast limits bound the work and the extrapolation so a caller cannot ask for
// an unbounded computation or a horizon longer than the data could ever support.
const (
	// MinHistoryPeriods is the least history a forecast is computed from. Below
	// this the answer would be a guess dressed as a number.
	MinHistoryPeriods = 3
	// MaxHistoryPeriods caps the observed series a single forecast walks.
	MaxHistoryPeriods = 240
	// MaxForecastHorizon caps how far ahead a forecast projects.
	MaxForecastHorizon = 36
	// MaxMovingAverageWindow caps the moving-average window.
	MaxMovingAverageWindow = 60
)

// PeriodPoint is one observed period of a metric. Period is a label the reader
// chose (for example "2026-01"); the core never parses it, it only reports it
// back, so the same series can be daily, weekly or monthly without a change here.
type PeriodPoint struct {
	Period string `json:"period"`
	Value  Money  `json:"value"`
}

// ForecastParams configures a forecast.
type ForecastParams struct {
	Method ForecastMethod
	// Window is the moving-average window. Ignored by the other methods.
	Window int
	// Horizon is the number of future periods to project, starting at the period
	// after the last observed one.
	Horizon int
}

// ForecastPoint is one projected period.
type ForecastPoint struct {
	Period string `json:"period"`
	Value  Money  `json:"value"`
	// Lower and Upper are the projection plus and minus the observed out-of-sample
	// mean absolute error. They are an *empirical error band*, not a statistical
	// prediction interval, and the field names say so; the UI labels them the same
	// way.
	Lower Money `json:"lower"`
	Upper Money `json:"upper"`
}

// ForecastQuality is the measured out-of-sample accuracy of a method on the
// series it was fitted to.
type ForecastQuality struct {
	Method ForecastMethod `json:"method"`
	// Folds is the number of out-of-sample points scored.
	Folds int `json:"folds"`
	// MAE is the mean absolute error across the folds, in the series currency.
	MAE Money `json:"mae"`
	// RMSE is the root-mean-square error, in the series currency.
	RMSE Money `json:"rmse"`
	// MAPE is the mean absolute percentage error in basis points. It is only
	// defined over folds whose actual value was non-zero; FoldsWithZeroActual
	// counts the folds excluded for that reason.
	MAPE                Ratio `json:"mape"`
	FoldsWithZeroActual int   `json:"folds_with_zero_actual"`
	// NaiveMAE and NaiveMAPE are the same metrics for the naive baseline that
	// carries the previous observed value forward. A method is only useful if it
	// beats this.
	NaiveMAE  Money `json:"naive_mae"`
	NaiveMAPE Ratio `json:"naive_mape"`
	// BeatsNaive is true when the method's MAE is lower than the naive baseline's.
	BeatsNaive bool `json:"beats_naive"`
}

// Forecast is the full result of a forecast.
type Forecast struct {
	Method           ForecastMethod `json:"method"`
	AlgorithmVersion string         `json:"algorithm_version"`
	Currency         string         `json:"currency"`
	// History is the observed series the forecast was computed from, echoed back
	// so a stored forecast is self-contained.
	History []PeriodPoint   `json:"history"`
	Points  []ForecastPoint `json:"points"`
	Quality ForecastQuality `json:"quality"`
	// Assumptions and Warnings are stated so the reader knows what the number does
	// and does not mean.
	Assumptions []string `json:"assumptions"`
	Warnings    []string `json:"warnings"`
}

// ErrForecastInsufficientData is returned when the series is too short to forecast
// or to score out of sample. It is a first-class result: the caller reports "not
// enough history" rather than a number built from nothing.
var ErrForecastInsufficientData = errors.New("insufficient history to forecast")

// RunForecast computes a forecast and its out-of-sample quality from an observed
// series. The series is expected in chronological order; it is sorted by nothing
// but its given order, because the period label is the caller's and only they
// know its ordering.
func RunForecast(history []PeriodPoint, p ForecastParams) (*Forecast, error) {
	if !p.Method.Valid() {
		return nil, fmt.Errorf("%w: unknown forecast method %q", ErrInvalidParameter, p.Method)
	}
	if len(history) > MaxHistoryPeriods {
		return nil, fmt.Errorf("%w: %d periods exceeds the %d limit",
			ErrInvalidParameter, len(history), MaxHistoryPeriods)
	}
	if len(history) < MinHistoryPeriods {
		return nil, fmt.Errorf("forecast: %w: %d periods, at least %d are needed",
			ErrForecastInsufficientData, len(history), MinHistoryPeriods)
	}
	if p.Horizon <= 0 {
		return nil, fmt.Errorf("%w: horizon must be positive", ErrInvalidParameter)
	}
	if p.Horizon > MaxForecastHorizon {
		return nil, fmt.Errorf("%w: horizon %d exceeds the %d limit",
			ErrInvalidParameter, p.Horizon, MaxForecastHorizon)
	}
	if p.Method == MethodMovingAverage {
		if p.Window <= 0 {
			return nil, fmt.Errorf("%w: a moving average needs a positive window", ErrInvalidParameter)
		}
		if p.Window > MaxMovingAverageWindow {
			return nil, fmt.Errorf("%w: window %d exceeds the %d limit",
				ErrInvalidParameter, p.Window, MaxMovingAverageWindow)
		}
		if p.Window > len(history) {
			return nil, fmt.Errorf("%w: window %d is longer than the %d observed periods",
				ErrInvalidParameter, p.Window, len(history))
		}
	}
	currency := history[0].Currency()
	if err := sameCurrencySeries(history); err != nil {
		return nil, err
	}

	values := make([]int64, len(history))
	for i, pt := range history {
		values[i] = pt.Value.AmountMinor
	}

	quality, err := backtest(values, currency, p)
	if err != nil {
		return nil, err
	}

	fitted, err := fit(values, p)
	if err != nil {
		return nil, err
	}

	f := &Forecast{
		Method:           p.Method,
		AlgorithmVersion: ForecastAlgorithmVersion,
		Currency:         currency,
		History:          append([]PeriodPoint(nil), history...),
		Quality:          quality,
		Assumptions: []string{
			"The forecast extends the observed series with a deterministic method; it is not a statistical model and carries no probability.",
			"Quality is measured out of sample: the method is fitted on a prefix of the series and scored on the next point, walking forward, so no scored point was in its own training data.",
			"The error band is the projection plus and minus the observed mean absolute error. It is an empirical error range, not a prediction interval.",
		},
	}

	lastPeriod := history[len(history)-1].Period
	for h := 1; h <= p.Horizon; h++ {
		projected, err := fitted.project(len(values) - 1 + h)
		if err != nil {
			return nil, err
		}
		period, err := nextPeriodLabel(lastPeriod, h)
		if err != nil {
			return nil, err
		}
		value := Money{AmountMinor: projected, Currency: currency}
		lo := Money{AmountMinor: projected - quality.MAE.AmountMinor, Currency: currency}
		if lo.AmountMinor < 0 {
			lo.AmountMinor = 0
		}
		hi := Money{AmountMinor: projected + quality.MAE.AmountMinor, Currency: currency}
		f.Points = append(f.Points, ForecastPoint{
			Period: period, Value: value, Lower: lo, Upper: hi,
		})
	}

	if p.Method == MethodTrend {
		f.Warnings = append(f.Warnings,
			"a trend forecast assumes the observed direction continues; a turning point is not anticipated")
	}
	if quality.Folds == 0 {
		f.Warnings = append(f.Warnings,
			"the series was too short to score out of sample, so no accuracy was measured")
	} else if !quality.BeatsNaive {
		f.Warnings = append(f.Warnings,
			"the method did not beat the naive baseline on this series; the naive figure is shown for comparison")
	}
	if quality.FoldsWithZeroActual > 0 {
		f.Warnings = append(f.Warnings,
			fmt.Sprintf("%d observed period(s) had a zero value and were excluded from the percentage error",
				quality.FoldsWithZeroActual))
	}
	return f, nil
}

// sameCurrencySeries rejects a series that mixes currencies: a total across
// unlike currencies is not a number anyone can act on.
func sameCurrencySeries(history []PeriodPoint) error {
	cur := history[0].Currency()
	for _, pt := range history {
		if c := pt.Currency(); c != cur {
			return fmt.Errorf("%w: the series mixes %s and %s", ErrInvalidParameter, cur, c)
		}
	}
	return nil
}

// Currency returns the point's currency, defaulting the empty value to USD so a
// zero-value Money behaves like the rest of the package.
func (p PeriodPoint) Currency() string { return normalizeCurrency(p.Value.Currency) }

// fitted is a method fitted to a whole series, able to project an arbitrary
// index. For the mean and moving average the projection ignores the index (the
// value is flat); for the trend it is evaluated on the fitted line.
type fitted struct {
	method ForecastMethod
	// flat is the constant projection for the mean and moving-average methods.
	flat int64
	// intercept and slope describe the trend line as rationals, evaluated as
	// intercept + slope*x with x the period index.
	intercept *big.Rat
	slope     *big.Rat
}

// fit fits the chosen method to the whole series.
func fit(values []int64, p ForecastParams) (*fitted, error) {
	switch p.Method {
	case MethodMean:
		return &fitted{method: MethodMean, flat: meanRounded(values)}, nil
	case MethodMovingAverage:
		window := values[len(values)-p.Window:]
		return &fitted{method: MethodMovingAverage, flat: meanRounded(window)}, nil
	case MethodTrend:
		a, b, err := trendLine(values)
		if err != nil {
			return nil, err
		}
		return &fitted{method: MethodTrend, intercept: a, slope: b}, nil
	default:
		return nil, fmt.Errorf("%w: unknown forecast method %q", ErrInvalidParameter, p.Method)
	}
}

// project returns the projected value at index x (0-based over the whole series).
func (f *fitted) project(x int) (int64, error) {
	switch f.method {
	case MethodMean, MethodMovingAverage:
		return f.flat, nil
	case MethodTrend:
		// value = intercept + slope*x, computed exactly and rounded once.
		v := new(big.Rat).Add(f.intercept, new(big.Rat).Mul(f.slope, new(big.Rat).SetInt64(int64(x))))
		r := ratRoundHalfAwayFromZero(v)
		if !r.IsInt64() {
			return 0, errors.New("forecast value overflow")
		}
		// A negative projection is not a real revenue; clamp at zero and let the
		// caller warn. This only bites a steep downward trend.
		if r.Sign() < 0 {
			return 0, nil
		}
		return r.Int64(), nil
	default:
		return 0, fmt.Errorf("%w: unknown forecast method", ErrInvalidParameter)
	}
}

// backtest scores the method out of sample with an expanding-window,
// one-step-ahead walk. For each index i from the first scoreable point to the
// end, it fits on values[:i] and predicts values[i]. Nothing scored was in its
// own fit.
func backtest(values []int64, currency string, p ForecastParams) (ForecastQuality, error) {
	q := ForecastQuality{Method: p.Method}

	start := MinHistoryPeriods
	if p.Method == MethodMovingAverage {
		start = p.Window
	}
	// The first scoreable index must have at least the method's minimum history
	// behind it.
	if start < 2 {
		start = 2
	}
	if start >= len(values) {
		return q, nil
	}

	var absErr, sqErr int64
	var pctSum int64
	var pctCount int
	var naiveAbsErr, naiveSqErr int64
	var naivePctSum int64

	for i := start; i < len(values); i++ {
		train := values[:i]
		f, err := fit(train, p)
		if err != nil {
			return q, err
		}
		predicted, err := f.project(i)
		if err != nil {
			return q, err
		}
		actual := values[i]
		diff := actual - predicted
		abs := absInt64(diff)
		absErr += abs
		sqErr += diff * diff

		// The naive baseline carries the previous observed value forward.
		naive := values[i-1]
		nDiff := actual - naive
		nAbs := absInt64(nDiff)
		naiveAbsErr += nAbs
		naiveSqErr += nDiff * nDiff

		if actual != 0 {
			pctSum += (abs * ratioScale) / absInt64(actual)
			naivePctSum += (nAbs * ratioScale) / absInt64(actual)
			pctCount++
		} else {
			q.FoldsWithZeroActual++
		}
		q.Folds++
	}

	if q.Folds > 0 {
		q.MAE = Money{AmountMinor: absErr / int64(q.Folds), Currency: currency}
		q.RMSE = Money{AmountMinor: isqrt(sqErr / int64(q.Folds)), Currency: currency}
		q.NaiveMAE = Money{AmountMinor: naiveAbsErr / int64(q.Folds), Currency: currency}
		q.BeatsNaive = q.MAE.AmountMinor < q.NaiveMAE.AmountMinor
	}
	if pctCount > 0 {
		q.MAPE = Ratio{BasisPoints: pctSum / int64(pctCount)}
		q.NaiveMAPE = Ratio{BasisPoints: naivePctSum / int64(pctCount)}
	}
	return q, nil
}

// trendLine returns the least-squares intercept and slope of values against their
// index, exactly, as rationals. Index x runs 0..n-1.
func trendLine(values []int64) (*big.Rat, *big.Rat, error) {
	n := int64(len(values))
	if n < 2 {
		return nil, nil, fmt.Errorf("%w: a trend needs at least two periods", ErrForecastInsufficientData)
	}
	// Centred sums keep the arithmetic small and exact:
	//   x̄ = (n-1)/2,  Sxx = Σ(x-x̄)²,  Sxy = Σ(x-x̄)(y-ȳ),  slope = Sxy/Sxx
	// and intercept is derived from ȳ = a + b*x̄.
	var sumY int64
	for _, v := range values {
		sumY += v
	}
	meanY := big.NewRat(sumY, n)
	meanX := big.NewRat(n-1, 2)

	sxx := new(big.Rat)
	sxy := new(big.Rat)
	for i, v := range values {
		dx := new(big.Rat).Sub(big.NewRat(int64(i), 1), meanX)
		dy := new(big.Rat).Sub(big.NewRat(v, 1), meanY)
		sxx.Add(sxx, new(big.Rat).Mul(dx, dx))
		sxy.Add(sxy, new(big.Rat).Mul(dx, dy))
	}
	if sxx.Sign() == 0 {
		// All x are identical, which cannot happen with a distinct index, but a
		// guard keeps a divide-by-zero from escaping as a panic.
		return nil, nil, fmt.Errorf("%w: degenerate trend fit", ErrInvalidParameter)
	}
	slope := new(big.Rat).Quo(sxy, sxx)
	// intercept = ȳ - slope*x̄
	intercept := new(big.Rat).Sub(meanY, new(big.Rat).Mul(slope, meanX))
	return intercept, slope, nil
}

// meanRounded returns the mean of the values, rounded half away from zero.
func meanRounded(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sum := new(big.Int)
	for _, v := range values {
		sum.Add(sum, big.NewInt(v))
	}
	return divRoundHalfAwayFromZero(sum, big.NewInt(int64(len(values)))).Int64()
}

// ratRoundHalfAwayFromZero rounds a rational to the nearest integer, halves away
// from zero. It is the single rounding boundary for the trend projection.
func ratRoundHalfAwayFromZero(r *big.Rat) *big.Int {
	num := new(big.Int).Set(r.Num())
	den := new(big.Int).Set(r.Denom()) // always positive for a Rat
	return divRoundHalfAwayFromZero(num, den)
}

// absInt64 returns the absolute value of v.
func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// isqrt returns the integer square root of a non-negative value, rounded to the
// nearest integer. It is used for RMSE so the whole path stays integer; a float
// square root would be the one place a float crept back into a money figure.
func isqrt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	x := n
	y := (x + 1) / 2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	// x is floor(sqrt(n)); round to nearest by comparing against the midpoint.
	if (x+1)*(x+1)-n <= n-x*x {
		return x + 1
	}
	return x
}

// nextPeriodLabel returns the label h periods after last. It understands the
// common "YYYY-MM" calendar form and, for any other shape, falls back to
// "last+h" so a caller using opaque labels still gets distinct, ordered ones.
func nextPeriodLabel(last string, h int) (string, error) {
	year, month, ok := parseYearMonth(last)
	if !ok {
		return fmt.Sprintf("%s+%d", last, h), nil
	}
	total := year*12 + (month - 1) + h
	return fmt.Sprintf("%04d-%02d", total/12, total%12+1), nil
}

// parseYearMonth parses a "YYYY-MM" label.
func parseYearMonth(s string) (year, month int, ok bool) {
	if len(s) != 7 || s[4] != '-' {
		return 0, 0, false
	}
	for i, r := range s {
		if i == 4 {
			continue
		}
		if r < '0' || r > '9' {
			return 0, 0, false
		}
	}
	y := 0
	for i := 0; i < 4; i++ {
		y = y*10 + int(s[i]-'0')
	}
	m := int(s[5]-'0')*10 + int(s[6]-'0')
	if m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, m, true
}

// SortPeriods orders points by their "YYYY-MM" label when every label parses,
// leaving any other series untouched. A reader that has already ordered its
// series does not need this; it is a convenience for callers that collected
// points out of order.
func SortPeriods(points []PeriodPoint) {
	for _, pt := range points {
		if _, _, ok := parseYearMonth(pt.Period); !ok {
			return
		}
	}
	sort.SliceStable(points, func(i, j int) bool { return points[i].Period < points[j].Period })
}
