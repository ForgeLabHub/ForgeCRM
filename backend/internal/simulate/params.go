package simulate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geff1778/forgecrm/internal/analytics"
)

// The parameter wire format is a small JSON object per scenario kind. Percentages
// are changes relative to 1.0: 20 means "20% more", -5 means "5% less". A value
// may be a JSON number or a numeric string, so a UI form that sends strings works
// without a separate shape.
//
// Every value is optional and defaults to "no change", so a scenario with no
// parameters computes the baseline against itself and reports zero deltas. That
// is the identity a user expects, and it makes an empty parameter set valid
// rather than an error.

// numOrString is a JSON number or numeric string.
type numOrString string

func (n *numOrString) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	if s == "null" {
		return nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		var inner string
		if err := json.Unmarshal(data, &inner); err != nil {
			return err
		}
		*n = numOrString(strings.TrimSpace(inner))
		return nil
	}
	*n = numOrString(s)
	return nil
}

// funnelParamsJSON is the funnel wire shape.
type funnelParamsJSON struct {
	LeadVolume       *numOrString           `json:"lead_volume"`
	StageConversions map[string]numOrString `json:"stage_conversions"`
}

// revenueParamsJSON is the revenue wire shape.
type revenueParamsJSON struct {
	AvgTicket *numOrString `json:"avg_ticket"`
	WinRate   *numOrString `json:"win_rate"`
	DealCount *numOrString `json:"deal_count"`
}

// retentionParamsJSON is the retention wire shape.
type retentionParamsJSON struct {
	ChurnMultiplier *numOrString `json:"churn_multiplier"`
	CustomerValue   *string      `json:"customer_value"`
}

// parseParams decodes the stored parameters for a kind into the concrete
// analytics parameter struct. It returns an error wrapping ErrValidation for a
// malformed value, so the API can answer 422 rather than 500.
func parseParams(kind analytics.Kind, raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	switch kind {
	case analytics.KindFunnel:
		return parseFunnelParams(raw)
	case analytics.KindRevenue:
		return parseRevenueParams(raw)
	case analytics.KindRetention:
		return parseRetentionParams(raw)
	default:
		return nil, fmt.Errorf("%w: unknown scenario kind %q", ErrValidation, kind)
	}
}

func parseFunnelParams(raw json.RawMessage) (analytics.FunnelParams, error) {
	var w funnelParamsJSON
	if err := decodeParams(raw, &w); err != nil {
		return analytics.FunnelParams{}, err
	}
	out := analytics.FunnelParams{}
	if w.LeadVolume != nil {
		r, err := percentToRatio("lead_volume", *w.LeadVolume)
		if err != nil {
			return analytics.FunnelParams{}, err
		}
		out.LeadVolume = r
	}
	if len(w.StageConversions) > 0 {
		out.StageConversions = make(map[string]analytics.Ratio, len(w.StageConversions))
		for stageID, n := range w.StageConversions {
			r, err := percentToRatio("stage_conversions."+stageID, n)
			if err != nil {
				return analytics.FunnelParams{}, err
			}
			out.StageConversions[stageID] = r
		}
	}
	return out, nil
}

func parseRevenueParams(raw json.RawMessage) (analytics.RevenueParams, error) {
	var w revenueParamsJSON
	if err := decodeParams(raw, &w); err != nil {
		return analytics.RevenueParams{}, err
	}
	out := analytics.RevenueParams{}
	if w.AvgTicket != nil {
		r, err := percentToRatio("avg_ticket", *w.AvgTicket)
		if err != nil {
			return analytics.RevenueParams{}, err
		}
		out.AvgTicket = r
	}
	if w.WinRate != nil {
		r, err := percentToRatio("win_rate", *w.WinRate)
		if err != nil {
			return analytics.RevenueParams{}, err
		}
		out.WinRate = r
	}
	if w.DealCount != nil {
		r, err := percentToRatio("deal_count", *w.DealCount)
		if err != nil {
			return analytics.RevenueParams{}, err
		}
		out.DealCount = r
	}
	return out, nil
}

func parseRetentionParams(raw json.RawMessage) (analytics.RetentionParams, error) {
	var w retentionParamsJSON
	if err := decodeParams(raw, &w); err != nil {
		return analytics.RetentionParams{}, err
	}
	out := analytics.RetentionParams{}
	if w.ChurnMultiplier != nil {
		r, err := percentToRatio("churn_multiplier", *w.ChurnMultiplier)
		if err != nil {
			return analytics.RetentionParams{}, err
		}
		out.ChurnMultiplier = r
	}
	if w.CustomerValue != nil && *w.CustomerValue != "" {
		m, err := analytics.ParseMoney(*w.CustomerValue, "USD")
		if err != nil {
			return analytics.RetentionParams{}, fmt.Errorf("%w: customer_value: %v", ErrValidation, err)
		}
		out.CustomerValue = &m
	}
	return out, nil
}

// decodeParams decodes into dst, rejecting an unknown field so a typo in a
// parameter name is reported rather than silently ignored.
func decodeParams(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: %v", ErrValidation, err)
	}
	return nil
}

// percentToRatio converts a percentage change into an exact Ratio.
func percentToRatio(name string, n numOrString) (analytics.Ratio, error) {
	s := strings.TrimSpace(string(n))
	if s == "" {
		return analytics.Ratio{}, fmt.Errorf("%w: %s must be a number", ErrValidation, name)
	}
	r, err := analytics.NewRatioFromPercent(s)
	if err != nil {
		return analytics.Ratio{}, fmt.Errorf("%w: %s: %v", ErrValidation, name, err)
	}
	return r, nil
}
