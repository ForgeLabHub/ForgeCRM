// Command smoke exercises the extracted analytics package with a deterministic
// input.
//
// It is the "does the extracted module actually run" step: a package can compile
// and still be useless, so the smoke builds a small pipeline and computes every
// scenario, printing the outcome. The input is fixed, so a change in the printed
// numbers is a real change in behaviour. The program exits non-zero if any
// scenario errors, which is what the harness checks.
package main

import (
	"fmt"
	"os"

	"github.com/geff1778/forgecrm/internal/analytics"
)

func main() {
	baseline := analytics.Baseline{
		Currency: "USD",
		Stages: []analytics.StageSnapshot{
			{StageID: "s1", StageName: "Qualified", Position: 1, OpenCount: 100, OpenValue: analytics.NewMoney(100000, "USD"), Probability: analytics.OneRatio},
			{StageID: "s2", StageName: "Proposal", Position: 2, OpenCount: 50, OpenValue: analytics.NewMoney(50000, "USD"), Probability: analytics.NewRatioFromBasisPoints(5000)},
			{StageID: "s3", StageName: "Won", Position: 3, OpenCount: 25, OpenValue: analytics.NewMoney(25000, "USD"), Probability: analytics.NewRatioFromBasisPoints(2500)},
		},
		WonCount:   10,
		LostCount:  5,
		OpenCount:  175,
		WonValue:   analytics.NewMoney(1000000, "USD"),
		LostValue:  analytics.NewMoney(300000, "USD"),
		OpenValue:  analytics.NewMoney(1750000, "USD"),
		PeriodDays: 30,
	}

	funnel, err := analytics.RunFunnel(baseline, analytics.FunnelParams{
		LeadVolume: analytics.NewRatioFromBasisPoints(11000),
	})
	check("funnel", err)

	revenue, err := analytics.RunRevenue(baseline, analytics.RevenueParams{
		AvgTicket: analytics.NewRatioFromBasisPoints(10500),
		WinRate:   analytics.NewRatioFromBasisPoints(11000),
		DealCount: analytics.NewRatioFromBasisPoints(11000),
	})
	check("revenue", err)

	fmt.Printf("smoke: funnel_metrics=%d revenue_metrics=%d\n",
		len(funnel.Metrics), len(revenue.Metrics))
}

func check(kind string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "scenario %s: %v\n", kind, err)
		os.Exit(1)
	}
}
