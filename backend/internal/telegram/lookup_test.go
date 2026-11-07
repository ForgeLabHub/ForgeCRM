package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

type lookupFake struct {
	results   []crm.SearchResult
	deals     []crm.Deal
	pipelines []crm.Pipeline
	report    *crm.PipelineReport
	err       error

	searchTerm string
	dealQuery  crm.ListQuery
	reportID   uuid.UUID
}

func (f *lookupFake) Search(_ context.Context, _ *rbac.Principal, term string, limit int) ([]crm.SearchResult, error) {
	f.searchTerm = term
	if limit != lookupLimit {
		panic("unexpected limit")
	}
	return f.results, f.err
}

func (f *lookupFake) ListDeals(_ context.Context, _ *rbac.Principal, q crm.ListQuery) ([]crm.Deal, int64, error) {
	f.dealQuery = q
	return f.deals, int64(len(f.deals)), f.err
}

func (f *lookupFake) ListPipelines(_ context.Context, _ *rbac.Principal) ([]crm.Pipeline, error) {
	return f.pipelines, f.err
}

func (f *lookupFake) PipelineReportFor(_ context.Context, _ *rbac.Principal, id uuid.UUID) (*crm.PipelineReport, error) {
	f.reportID = id
	return f.report, f.err
}

// principalWith builds a principal whose permission set is exactly the given
// permissions, so a test can prove the bot refuses a command without the right.
func principalWith(perms ...rbac.Permission) *rbac.Principal {
	return rbac.NewPrincipal(uuid.New(), "", "", uuid.New(), perms)
}

func TestSearchCommand(t *testing.T) {
	f := &lookupFake{results: []crm.SearchResult{
		{EntityType: "deal", Title: "Big\nDeal", Subtitle: "USD 10.00"},
		{EntityType: "account", Title: "Acme"},
	}}
	p := principalWith(rbac.PermDealsRead)
	text, err := handleLookupCommand(context.Background(), f, p, "/search Acme Corp", telegramtext.LocaleEnglish)
	if err != nil || f.searchTerm != "Acme Corp" {
		t.Fatalf("search: %q %v term=%q", text, err, f.searchTerm)
	}
	if !strings.Contains(text, "Big Deal") || strings.Contains(text, "\nDeal") {
		t.Fatalf("title not sanitized: %q", text)
	}
	if !strings.Contains(text, "[account] Acme") {
		t.Fatalf("missing account line: %q", text)
	}
}

func TestSearchCommandNeedsATerm(t *testing.T) {
	f := &lookupFake{}
	for _, in := range []string{"/search", "/search a"} {
		out, err := handleLookupCommand(context.Background(), f, principalWith(), in, telegramtext.LocaleEnglish)
		if err != nil || out != telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeySearchUsage) {
			t.Fatalf("%s: %q %v", in, out, err)
		}
		if f.searchTerm != "" {
			t.Fatalf("%s: search was called", in)
		}
	}
}

func TestDealsCommandRequiresPermission(t *testing.T) {
	f := &lookupFake{deals: []crm.Deal{{Title: "Renewal", Amount: 1500, Currency: "usd", Status: "open"}}}
	// Without deals.read the command is refused before any query runs.
	out, err := handleLookupCommand(context.Background(), f, principalWith(), "/deals", telegramtext.LocaleEnglish)
	if err != nil || out != telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyNoPermission) {
		t.Fatalf("no permission: %q %v", out, err)
	}
	if f.dealQuery.Status != "" && f.dealQuery.Status != "open" {
		t.Fatalf("query ran despite missing permission: %+v", f.dealQuery)
	}

	out, err = handleLookupCommand(context.Background(), f, principalWith(rbac.PermDealsRead), "/deals open", telegramtext.LocaleEnglish)
	if err != nil || f.dealQuery.Status != "open" || f.dealQuery.Pagination.Limit != lookupLimit {
		t.Fatalf("deals: %q %v %+v", out, err, f.dealQuery)
	}
	if !strings.Contains(out, "USD 1500.00") || !strings.Contains(out, "Renewal") {
		t.Fatalf("deal line: %q", out)
	}
}

func TestDealsCommandRejectsUnknownStatus(t *testing.T) {
	f := &lookupFake{}
	out, err := handleLookupCommand(context.Background(), f, principalWith(rbac.PermDealsRead), "/deals nonsense", telegramtext.LocaleEnglish)
	if err != nil || out != telegramtext.T(telegramtext.LocaleEnglish, telegramtext.KeyUsageDeals) {
		t.Fatalf("bad status: %q %v", out, err)
	}
}

func TestPipelineCommandUsesDefault(t *testing.T) {
	def := uuid.New()
	f := &lookupFake{
		pipelines: []crm.Pipeline{{ID: uuid.New(), Name: "Other"}, {ID: def, Name: "Sales", IsDefault: true, Currency: "USD"}},
		report: &crm.PipelineReport{
			PipelineName: "Sales", Currency: "USD", TotalOpen: 250,
			Stages: []crm.PipelineStageStat{{StageName: "Lead", DealCount: 2, TotalAmount: 100}, {StageName: "Won", DealCount: 0}},
		},
	}
	out, err := handleLookupCommand(context.Background(), f, principalWith(rbac.PermPipelinesRead), "/pipeline", telegramtext.LocaleEnglish)
	if err != nil || f.reportID != def {
		t.Fatalf("pipeline default: %q %v id=%s", out, err, f.reportID)
	}
	if !strings.Contains(out, "Lead") || strings.Contains(out, "Won") {
		t.Fatalf("stages: %q", out)
	}
	if !strings.Contains(out, "USD 250.00") {
		t.Fatalf("total: %q", out)
	}
}

func TestPipelineCommandSelectsByName(t *testing.T) {
	want := uuid.New()
	f := &lookupFake{
		pipelines: []crm.Pipeline{{ID: uuid.New(), Name: "Enterprise", IsDefault: true}, {ID: want, Name: "Renewals"}},
		report:    &crm.PipelineReport{PipelineName: "Renewals", Currency: "EUR", Stages: []crm.PipelineStageStat{{StageName: "Active", DealCount: 1, TotalAmount: 5}}},
	}
	_, err := handleLookupCommand(context.Background(), f, principalWith(rbac.PermPipelinesRead), "/pipeline renew", telegramtext.LocaleEnglish)
	if err != nil || f.reportID != want {
		t.Fatalf("select by name: %v id=%s", err, f.reportID)
	}
}

func TestLookupCommandHidesErrors(t *testing.T) {
	f := &lookupFake{err: errors.New("sensitive database error")}
	out, err := handleLookupCommand(context.Background(), f, principalWith(rbac.PermDealsRead), "/deals", telegramtext.LocaleEnglish)
	if err == nil || out != "" {
		t.Fatalf("error exposed: %q %v", out, err)
	}
}
