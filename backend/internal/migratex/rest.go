package migratex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/integrations"
)

// restSource reads records from a REST API.
//
// Every request goes through the SSRF guard, so a migration cannot be used to
// reach an internal service or a cloud metadata endpoint.
type restSource struct {
	cfg    ConnectionConfig
	apiKey string
	client *integrations.GuardedClient
}

// NewRESTSource builds a REST migration source.
func NewRESTSource(cfg ConnectionConfig, apiKey string) (Source, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &restSource{
		cfg:    cfg,
		apiKey: apiKey,
		client: integrations.NewGuardedClient(30 * time.Second),
	}, nil
}

func (s *restSource) Kind() SourceKind { return SourceREST }

func (s *restSource) TestConnection(ctx context.Context) error {
	if _, err := integrations.ValidateOutboundURL(ctx, s.cfg.BaseURL); err != nil {
		return err
	}
	req, err := s.newRequest(ctx, http.MethodGet, s.cfg.BaseURL)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("REST source is unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("REST source returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (s *restSource) newRequest(ctx context.Context, method, target string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if s.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.apiKey)
	}
	return req, nil
}

// DiscoverSchema reports a single logical resource, because a generic REST
// endpoint has no discoverable table structure. The operator supplies an explicit
// mapping, and this makes that requirement visible in the UI.
func (s *restSource) DiscoverSchema(ctx context.Context) ([]TableSchema, error) {
	path := s.cfg.ReadPath
	if path == "" {
		path = "/"
	}
	return []TableSchema{{
		Name: strings.Trim(path, "/"),
		Columns: []ColumnInfo{
			{Name: "records", Type: "array", Nullable: false},
		},
	}}, nil
}

func (s *restSource) CountRows(ctx context.Context, table string) (int64, error) {
	records, err := s.fetch(ctx, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(records)), nil
}

func (s *restSource) ReadBatch(ctx context.Context, table string, columns []string,
	watermark map[string]any, limit int) ([]map[string]any, error) {
	records, err := s.fetch(ctx, limit)
	if err != nil {
		return nil, err
	}
	if since, ok := watermark["value"]; ok && since != nil {
		col, _ := watermark["column"].(string)
		records = filterAfter(records, col, since)
	}
	return records, nil
}

func (s *restSource) fetch(ctx context.Context, limit int) ([]map[string]any, error) {
	base, err := url.Parse(s.cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base_url: %w", err)
	}
	target := base.JoinPath(s.cfg.ReadPath).String()

	req, err := s.newRequest(ctx, http.MethodGet, target)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("REST read failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("REST read returned HTTP %d", resp.StatusCode)
	}
	// Bound the response so a hostile or misconfigured endpoint cannot exhaust
	// memory.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, fmt.Errorf("read REST response: %w", err)
	}
	records, err := decodeRESTRecords(data)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

func decodeRESTRecords(data []byte) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "[") {
		var arr []map[string]any
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, fmt.Errorf("decode REST array: %w", err)
		}
		return arr, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("decode REST object: %w", err)
	}
	for _, key := range []string{"data", "items", "records", "rows", "results"} {
		if v, ok := obj[key]; ok && strings.HasPrefix(strings.TrimSpace(string(v)), "[") {
			var arr []map[string]any
			if err := json.Unmarshal(v, &arr); err != nil {
				return nil, fmt.Errorf("decode REST %s array: %w", key, err)
			}
			return arr, nil
		}
	}
	return nil, fmt.Errorf("the REST response does not contain an array of records")
}

func (s *restSource) Close() error { return nil }
