package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/telegramtext"
)

// lookupOperations is the read side of the CRM the bot exposes. Every method is
// already permission- and ownership-aware in the service, so the bot adds no
// access of its own: it renders what the linked user could see in the web app.
type lookupOperations interface {
	Search(context.Context, *rbac.Principal, string, int) ([]crm.SearchResult, error)
	ListDeals(context.Context, *rbac.Principal, crm.ListQuery) ([]crm.Deal, int64, error)
	ListPipelines(context.Context, *rbac.Principal) ([]crm.Pipeline, error)
	PipelineReportFor(context.Context, *rbac.Principal, uuid.UUID) (*crm.PipelineReport, error)
}

// lookupLimit bounds every list the bot renders. A chat message is a summary, not
// a report; a caller who needs more opens the web interface.
const lookupLimit = 10

// handleLookupCommand answers /search, /deals and /pipeline. It returns a
// localized reply, or an error the caller turns into a generic response. It never
// echoes the raw argument on an error path, so a malformed command cannot leak
// text into a reply or a log.
func handleLookupCommand(ctx context.Context, lookup lookupOperations, p *rbac.Principal, text string, loc telegramtext.Locale) (string, error) {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return telegramtext.T(loc, telegramtext.KeyUnknownCommand), nil
	}
	cmd := strings.ToLower(parts[0])
	if i := strings.IndexByte(cmd, '@'); i >= 0 {
		cmd = cmd[:i]
	}
	switch cmd {
	case "/search":
		if len(parts) < 2 {
			return telegramtext.T(loc, telegramtext.KeySearchUsage), nil
		}
		term := strings.Join(parts[1:], " ")
		if len([]rune(term)) < 2 {
			return telegramtext.T(loc, telegramtext.KeySearchUsage), nil
		}
		results, err := lookup.Search(ctx, p, term, lookupLimit)
		if err != nil {
			return "", err
		}
		if len(results) == 0 {
			return telegramtext.T(loc, telegramtext.KeySearchEmpty), nil
		}
		var b strings.Builder
		b.WriteString(telegramtext.T(loc, telegramtext.KeySearchHeader))
		for _, r := range results {
			line := safeTitle(r.Title)
			if r.Subtitle != "" {
				line += " — " + safeTitle(r.Subtitle)
			}
			fmt.Fprintf(&b, "\n• [%s] %s", safeTitle(r.EntityType), line)
		}
		return b.String(), nil
	case "/deals":
		if !p.Has(rbac.PermDealsRead) {
			return telegramtext.T(loc, telegramtext.KeyNoPermission), nil
		}
		status := ""
		label := "all"
		if len(parts) >= 2 {
			status = strings.ToLower(parts[1])
			switch status {
			case "open", "won", "lost":
				label = status
			default:
				return telegramtext.T(loc, telegramtext.KeyUsageDeals), nil
			}
		}
		deals, _, err := lookup.ListDeals(ctx, p, crm.ListQuery{
			Status:     status,
			Pagination: httpx.Pagination{Limit: lookupLimit},
		})
		if err != nil {
			return "", err
		}
		if len(deals) == 0 {
			return telegramtext.T(loc, telegramtext.KeyDealsEmpty), nil
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyDealsHeader), label))
		for _, d := range deals {
			fmt.Fprintf(&b, "\n• "+telegramtext.T(loc, telegramtext.KeyDealLine), safeTitle(d.Title), formatMoney(d.Amount, d.Currency), safeTitle(d.Status))
		}
		return b.String(), nil
	case "/pipeline":
		if !p.Has(rbac.PermPipelinesRead) {
			return telegramtext.T(loc, telegramtext.KeyNoPermission), nil
		}
		pipelines, err := lookup.ListPipelines(ctx, p)
		if err != nil {
			return "", err
		}
		if len(pipelines) == 0 {
			return telegramtext.T(loc, telegramtext.KeyPipelineEmpty), nil
		}
		want := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(text, parts[0])))
		chosen := pickPipeline(pipelines, want)
		if chosen == nil {
			return telegramtext.T(loc, telegramtext.KeyPipelineEmpty), nil
		}
		report, err := lookup.PipelineReportFor(ctx, p, chosen.ID)
		if err != nil {
			return "", err
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf(telegramtext.T(loc, telegramtext.KeyPipelineHeader), safeTitle(report.PipelineName)))
		wrote := false
		for _, st := range report.Stages {
			if st.DealCount == 0 {
				continue
			}
			wrote = true
			fmt.Fprintf(&b, "\n• "+telegramtext.T(loc, telegramtext.KeyPipelineLine), safeTitle(st.StageName), st.DealCount, formatMoney(st.TotalAmount, report.Currency))
		}
		if !wrote {
			return telegramtext.T(loc, telegramtext.KeyPipelineEmpty), nil
		}
		fmt.Fprintf(&b, "\n\nTotal: %s", formatMoney(report.TotalOpen, report.Currency))
		return b.String(), nil
	default:
		return telegramtext.T(loc, telegramtext.KeyUnknownCommand), nil
	}
}

// pickPipeline returns the pipeline whose name contains the wanted fragment, or
// the default pipeline when the caller named none. It returns nil only when there
// is nothing to choose from.
func pickPipeline(pipelines []crm.Pipeline, want string) *crm.Pipeline {
	if want != "" {
		for i := range pipelines {
			if strings.Contains(strings.ToLower(pipelines[i].Name), want) {
				return &pipelines[i]
			}
		}
		return nil
	}
	for i := range pipelines {
		if pipelines[i].IsDefault {
			return &pipelines[i]
		}
	}
	return &pipelines[0]
}

// formatMoney renders an amount with its currency. It keeps the currency even
// when it is empty, so a column of figures stays aligned.
func formatMoney(amount float64, currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		currency = "—"
	}
	return fmt.Sprintf("%s %.2f", currency, amount)
}
