package analytics

import (
	"errors"
	"testing"
)

// periods builds a monthly series from integer minor-unit values, labelled from
// 2026-01 forward, so the expected results can be checked by hand.
func periods(values ...int64) []PeriodPoint {
	out := make([]PeriodPoint, 0, len(values))
	for i, v := range values {
		month := i + 1
		out = append(out, PeriodPoint{
			Period: periodLabel(2026, month),
			Value:  NewMoney(v, "USD"),
		})
	}
	return out
}

func periodLabel(year, month int) string {
	return nextPeriodLabelUnsafe(year, month)
}

// nextPeriodLabelUnsafe mirrors the production formatter for test labels only.
func nextPeriodLabelUnsafe(year, month int) string {
	total := year*12 + (month - 1)
	return twoDigits(total/12, 4) + "-" + twoDigits(total%12+1, 2)
}

func twoDigits(v, width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s = string(rune('0'+v%10)) + s
		v /= 10
	}
	return s
}

func forecastByPeriod(t *testing.T, f *Forecast, period string) ForecastPoint {
	t.Helper()
	for _, pt := range f.Points {
		if pt.Period == period {
			return pt
		}
	}
	t.Fatalf("forecast has no point for %s: %+v", period, f.Points)
	return ForecastPoint{}
}

func TestForecastMeanOfFlatSeries(t *testing.T) {
	// A constant series: every method must reproduce the constant, and the naive
	// baseline must match exactly, so BeatsNaive is false (not better, not worse).
	f, err := RunForecast(periods(1000, 1000, 1000, 1000), ForecastParams{Method: MethodMean, Horizon: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range f.Points {
		if pt.Value.AmountMinor != 1000 {
			t.Errorf("mean forecast %s = %d, want 1000", pt.Period, pt.Value.AmountMinor)
		}
	}
	if f.Quality.MAE.AmountMinor != 0 {
		t.Errorf("MAE on a flat series = %d, want 0", f.Quality.MAE.AmountMinor)
	}
	if f.Quality.BeatsNaive {
		t.Errorf("a flat series must not report beating the naive baseline")
	}
}

func TestForecastMeanAverages(t *testing.T) {
	// mean(1000,2000,3000,4000) = 2500. Horizon extends the label.
	f, err := RunForecast(periods(1000, 2000, 3000, 4000), ForecastParams{Method: MethodMean, Horizon: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Points[0].Value.AmountMinor; got != 2500 {
		t.Errorf("mean forecast = %d, want 2500", got)
	}
	if f.Points[0].Period != "2026-05" {
		t.Errorf("first projected period = %s, want 2026-05", f.Points[0].Period)
	}
	if f.Points[1].Period != "2026-06" {
		t.Errorf("second projected period = %s, want 2026-06", f.Points[1].Period)
	}
}

func TestForecastMovingAverageUsesWindow(t *testing.T) {
	// window 2 over (100,200,300,400): last two are 300,400 -> mean 350.
	f, err := RunForecast(periods(100, 200, 300, 400), ForecastParams{
		Method: MethodMovingAverage, Window: 2, Horizon: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Points[0].Value.AmountMinor; got != 350 {
		t.Errorf("moving average = %d, want 350", got)
	}
}

func TestForecastTrendExtendsLine(t *testing.T) {
	// Perfectly linear series 100,200,300,400 (index 0..3). The line is
	// value = 100 + 100*x, so index 4 projects to 500.
	f, err := RunForecast(periods(100, 200, 300, 400), ForecastParams{Method: MethodTrend, Horizon: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Points[0].Value.AmountMinor; got != 500 {
		t.Errorf("trend projection at index 4 = %d, want 500", got)
	}
	if got := f.Points[1].Value.AmountMinor; got != 600 {
		t.Errorf("trend projection at index 5 = %d, want 600", got)
	}
	if f.Quality.MAE.AmountMinor != 0 {
		t.Errorf("trend on a perfect line should have zero error, got %d", f.Quality.MAE.AmountMinor)
	}
}

func TestForecastTrendClampsNegative(t *testing.T) {
	// A steep downward line: 400,300,200,100 -> index 4 projects to 0, index 5 to
	// -100, which is clamped to 0 rather than reported as negative revenue.
	f, err := RunForecast(periods(400, 300, 200, 100), ForecastParams{Method: MethodTrend, Horizon: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, pt := range f.Points {
		if pt.Value.AmountMinor < 0 {
			t.Errorf("trend projected a negative value %d at %s", pt.Value.AmountMinor, pt.Period)
		}
	}
}

func TestForecastBacktestIsOutOfSample(t *testing.T) {
	// A series that steps up at the end. The naive baseline carries the previous
	// value forward, so on a step it is perfectly right; the mean lags. The mean
	// must therefore not beat naive here, which is the honest answer.
	f, err := RunForecast(periods(1000, 1000, 1000, 1000, 5000), ForecastParams{
		Method: MethodMean, Horizon: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.Quality.Folds != 2 {
		// Folds start at index 2 (MinHistoryPeriods): indices 2,3,4 -> 3 folds.
		t.Errorf("folds = %d, want 3", f.Quality.Folds)
	}
	if f.Quality.BeatsNaive {
		t.Errorf("mean should not beat naive across a step; MAE=%d naiveMAE=%d",
			f.Quality.MAE.AmountMinor, f.Quality.NaiveMAE.AmountMinor)
	}
}

func TestForecastQualityMAPESkipsZeroActuals(t *testing.T) {
	// One actual is zero; it must be excluded from MAPE and counted.
	f, err := RunForecast(periods(100, 100, 100, 0, 100), ForecastParams{Method: MethodMean, Horizon: 1})
	if err != nil {
		t.Fatal(err)
	}
	if f.Quality.FoldsWithZeroActual != 1 {
		t.Errorf("zero-actual folds = %d, want 1", f.Quality.FoldsWithZeroActual)
	}
}

func TestForecastRejectsBadInput(t *testing.T) {
	cases := []struct {
		name string
		hist []PeriodPoint
		p    ForecastParams
		want error
	}{
		{"unknown method", periods(1, 2, 3), ForecastParams{Method: "magic", Horizon: 1}, ErrInvalidParameter},
		{"too short", periods(1, 2), ForecastParams{Method: MethodMean, Horizon: 1}, ErrForecastInsufficientData},
		{"zero horizon", periods(1, 2, 3), ForecastParams{Method: MethodMean, Horizon: 0}, ErrInvalidParameter},
		{"huge horizon", periods(1, 2, 3), ForecastParams{Method: MethodMean, Horizon: 9999}, ErrInvalidParameter},
		{"ma no window", periods(1, 2, 3), ForecastParams{Method: MethodMovingAverage, Horizon: 1}, ErrInvalidParameter},
		{"ma window too long", periods(1, 2, 3), ForecastParams{Method: MethodMovingAverage, Window: 5, Horizon: 1}, ErrInvalidParameter},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := RunForecast(c.hist, c.p)
			if !errors.Is(err, c.want) {
				t.Errorf("err = %v, want %v", err, c.want)
			}
		})
	}
}

func TestForecastRejectsMixedCurrencies(t *testing.T) {
	hist := []PeriodPoint{
		{Period: "2026-01", Value: NewMoney(100, "USD")},
		{Period: "2026-02", Value: NewMoney(100, "EUR")},
		{Period: "2026-03", Value: NewMoney(100, "USD")},
	}
	_, err := RunForecast(hist, ForecastParams{Method: MethodMean, Horizon: 1})
	if !errors.Is(err, ErrInvalidParameter) {
		t.Errorf("mixed currencies err = %v, want ErrInvalidParameter", err)
	}
}

func TestForecastDeterministic(t *testing.T) {
	hist := periods(1234, 2345, 3456, 4567, 5678)
	p := ForecastParams{Method: MethodTrend, Horizon: 3}
	a, err := RunForecast(hist, p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RunForecast(hist, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Points) != len(b.Points) {
		t.Fatalf("point counts differ")
	}
	for i := range a.Points {
		if a.Points[i].Value.AmountMinor != b.Points[i].Value.AmountMinor {
			t.Errorf("run differs at %d: %d vs %d", i, a.Points[i].Value.AmountMinor, b.Points[i].Value.AmountMinor)
		}
	}
}

func TestForecastErrorBandBracketsProjection(t *testing.T) {
	f, err := RunForecast(periods(1000, 2000, 3000, 4000), ForecastParams{Method: MethodMean, Horizon: 1})
	if err != nil {
		t.Fatal(err)
	}
	pt := f.Points[0]
	if pt.Lower.AmountMinor > pt.Value.AmountMinor || pt.Value.AmountMinor > pt.Upper.AmountMinor {
		t.Errorf("band does not bracket projection: lower=%d value=%d upper=%d",
			pt.Lower.AmountMinor, pt.Value.AmountMinor, pt.Upper.AmountMinor)
	}
}

func TestIsqrt(t *testing.T) {
	cases := map[int64]int64{0: 0, 1: 1, 2: 1, 4: 2, 9: 3, 10: 3, 15: 4, 16: 4, 17: 4, 25: 5}
	for in, want := range cases {
		if got := isqrt(in); got != want {
			t.Errorf("isqrt(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestNextPeriodLabelWrapsYear(t *testing.T) {
	cases := []struct {
		last string
		h    int
		want string
	}{
		{"2026-01", 1, "2026-02"},
		{"2026-12", 1, "2027-01"},
		{"2026-11", 3, "2027-02"},
		{"2026-06", 12, "2027-06"},
	}
	for _, c := range cases {
		got, err := nextPeriodLabel(c.last, c.h)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("nextPeriodLabel(%q,%d) = %q, want %q", c.last, c.h, got, c.want)
		}
	}
}

func TestNextPeriodLabelFallback(t *testing.T) {
	got, err := nextPeriodLabel("Q1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Q1+2" {
		t.Errorf("fallback label = %q, want Q1+2", got)
	}
}
