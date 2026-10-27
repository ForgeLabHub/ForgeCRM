package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/geff1778/forgecrm/internal/httpx"
)

// BuildAdapter constructs the adapter for an integration.
//
// Every kind is listed explicitly. An unknown kind is refused rather than
// silently treated as a no-op, so a typo in configuration cannot produce an
// integration that appears healthy while doing nothing.
func (h *Hub) BuildAdapter(ctx context.Context, i *Integration) (Adapter, error) {
	secretsMap, err := h.Secrets(ctx, i.ID, i.OrganizationID)
	if err != nil {
		return nil, err
	}
	cfg := map[string]any{}
	if len(i.Config) > 0 {
		if err := json.Unmarshal(i.Config, &cfg); err != nil {
			return nil, httpx.NewError(422, httpx.CodeValidation, "integration config is not valid JSON")
		}
	}
	timeout := time.Duration(i.TimeoutSeconds) * time.Second

	switch i.Kind {
	case "rest_api":
		return newRESTAdapter(cfg, secretsMap, h.guard, timeout)
	case "webhook_out":
		return newWebhookOutAdapter(cfg, secretsMap, h.guard, timeout)
	case "smtp_probe":
		return newSMTPProbeAdapter(cfg, secretsMap, timeout)
	case "postgres", "mysql", "sqlite", "mssql", "oracle":
		// Database sources are handled by the migration package, which owns the
		// read-only driver plumbing. The hub reports the kind as recognised so
		// configuration errors surface early.
		return newDatabaseDescriptorAdapter(i.Kind, cfg, secretsMap)
	default:
		return nil, httpx.NewError(422, httpx.CodeValidation,
			fmt.Sprintf("unknown integration kind %q", i.Kind))
	}
}

// ---------------------------------------------------------------------------
// REST adapter
// ---------------------------------------------------------------------------

type restAdapter struct {
	baseURL   string
	apiKey    string
	authStyle string
	guard     *GuardedClient
	timeout   time.Duration
	readPath  string
	writePath string
}

func newRESTAdapter(cfg map[string]any, secretsMap map[string]string,
	guard *GuardedClient, timeout time.Duration) (Adapter, error) {
	base, _ := cfg["base_url"].(string)
	if base == "" {
		return nil, httpx.NewError(422, httpx.CodeValidation, "rest_api integration requires config.base_url")
	}
	a := &restAdapter{
		baseURL:   strings.TrimRight(base, "/"),
		apiKey:    secretsMap["api_key"],
		authStyle: strOr(cfg["auth_style"], "bearer"),
		guard:     guard,
		timeout:   timeout,
		readPath:  strOr(cfg["read_path"], ""),
		writePath: strOr(cfg["write_path"], ""),
	}
	return a, nil
}

func (a *restAdapter) Kind() string { return "rest_api" }

func (a *restAdapter) ValidateConfig(ctx context.Context) error {
	if a.baseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	if _, err := ValidateOutboundURL(ctx, a.baseURL); err != nil {
		return err
	}
	switch a.authStyle {
	case "bearer", "header", "none":
	default:
		return fmt.Errorf("auth_style must be bearer, header or none")
	}
	if a.authStyle != "none" && a.apiKey == "" {
		return fmt.Errorf("an API key is required for auth_style %q", a.authStyle)
	}
	return nil
}

func (a *restAdapter) TestConnection(ctx context.Context) error {
	if err := a.ValidateConfig(ctx); err != nil {
		return err
	}
	// A read of the configured path is the only meaningful proof that the
	// credentials are accepted; a bare TCP connect would prove nothing.
	target := a.baseURL
	if a.readPath != "" {
		target = a.baseURL + "/" + strings.TrimLeft(a.readPath, "/")
	}
	req, err := newGuardedRequest(ctx, "GET", target, a.headers())
	if err != nil {
		return err
	}
	resp, err := a.guard.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("the endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (a *restAdapter) headers() map[string]string {
	h := map[string]string{"Accept": "application/json"}
	switch a.authStyle {
	case "bearer":
		h["Authorization"] = "Bearer " + a.apiKey
	case "header":
		h["X-Api-Key"] = a.apiKey
	}
	return h
}

func (a *restAdapter) DiscoverSchema(ctx context.Context) (Schema, error) {
	// Without a schema endpoint there is nothing to discover; report that
	// explicitly instead of inventing a structure.
	return Schema{}, fmt.Errorf("schema discovery is not supported by this REST endpoint; supply an explicit field mapping")
}

func (a *restAdapter) ReadRecords(ctx context.Context, req ReadRequest) ([]Record, error) {
	if a.readPath == "" {
		return nil, fmt.Errorf("config.read_path is required to read records")
	}
	target := a.baseURL + "/" + strings.TrimLeft(a.readPath, "/")
	greq, err := newGuardedRequest(ctx, "GET", target, a.headers())
	if err != nil {
		return nil, err
	}
	resp, err := a.guard.Do(greq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("read returned HTTP %d", resp.StatusCode)
	}
	var payload json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return decodeRecords(payload, req.Limit)
}

func (a *restAdapter) TransformRecords(ctx context.Context, records []Record, m map[string]string) ([]Record, error) {
	return transform(records, m), nil
}

func (a *restAdapter) WriteRecords(ctx context.Context, records []Record) (WriteResult, error) {
	return WriteResult{}, ErrWriteNotSupported
}

func (a *restAdapter) GetStatus(ctx context.Context) (Status, error) {
	if err := a.TestConnection(ctx); err != nil {
		return Status{Healthy: false, LastError: err.Error(), CheckedAt: time.Now()}, nil
	}
	return Status{Healthy: true, Message: "reachable", CheckedAt: time.Now()}, nil
}

func (a *restAdapter) Close() error { return nil }

// ---------------------------------------------------------------------------
// Outbound webhook adapter
// ---------------------------------------------------------------------------

type webhookOutAdapter struct {
	target string
	guard  *GuardedClient
}

func newWebhookOutAdapter(cfg map[string]any, secretsMap map[string]string,
	guard *GuardedClient, timeout time.Duration) (Adapter, error) {
	target, _ := cfg["target_url"].(string)
	if target == "" {
		return nil, httpx.NewError(422, httpx.CodeValidation,
			"webhook_out integration requires config.target_url")
	}
	return &webhookOutAdapter{target: target, guard: guard}, nil
}

func (a *webhookOutAdapter) Kind() string { return "webhook_out" }

func (a *webhookOutAdapter) ValidateConfig(ctx context.Context) error {
	_, err := ValidateOutboundURL(ctx, a.target)
	return err
}

func (a *webhookOutAdapter) TestConnection(ctx context.Context) error {
	// Validate the destination but do not send a test payload: sending real data
	// to an unverified endpoint is not something a connectivity test should do.
	return a.ValidateConfig(ctx)
}

func (a *webhookOutAdapter) DiscoverSchema(ctx context.Context) (Schema, error) {
	return Schema{}, fmt.Errorf("a webhook target has no discoverable schema")
}

func (a *webhookOutAdapter) ReadRecords(ctx context.Context, req ReadRequest) ([]Record, error) {
	return nil, fmt.Errorf("a webhook target cannot be read")
}

func (a *webhookOutAdapter) TransformRecords(ctx context.Context, records []Record, m map[string]string) ([]Record, error) {
	return transform(records, m), nil
}

func (a *webhookOutAdapter) WriteRecords(ctx context.Context, records []Record) (WriteResult, error) {
	return WriteResult{}, ErrWriteNotSupported
}

func (a *webhookOutAdapter) GetStatus(ctx context.Context) (Status, error) {
	if err := a.ValidateConfig(ctx); err != nil {
		return Status{Healthy: false, LastError: err.Error(), CheckedAt: time.Now()}, nil
	}
	return Status{Healthy: true, Message: "target URL is acceptable", CheckedAt: time.Now()}, nil
}

func (a *webhookOutAdapter) Close() error { return nil }

// ---------------------------------------------------------------------------
// SMTP probe adapter
// ---------------------------------------------------------------------------

type smtpProbeAdapter struct {
	host     string
	port     int
	username string
	password string
	timeout  time.Duration
}

func newSMTPProbeAdapter(cfg map[string]any, secretsMap map[string]string, timeout time.Duration) (Adapter, error) {
	host, _ := cfg["host"].(string)
	if host == "" {
		return nil, httpx.NewError(422, httpx.CodeValidation, "smtp_probe integration requires config.host")
	}
	return &smtpProbeAdapter{
		host:     host,
		port:     intOr(cfg["port"], 587),
		username: secretsMap["username"],
		password: secretsMap["password"],
		timeout:  timeout,
	}, nil
}

func (a *smtpProbeAdapter) Kind() string { return "smtp_probe" }

func (a *smtpProbeAdapter) ValidateConfig(ctx context.Context) error {
	if a.host == "" {
		return fmt.Errorf("host is required")
	}
	if a.port <= 0 || a.port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

func (a *smtpProbeAdapter) TestConnection(ctx context.Context) error {
	// A TCP reachability check only proves the port is open. The notification
	// package performs the authenticated test, and the two are deliberately kept
	// distinct so "reachable" is never reported as "authenticated".
	return probeTCP(ctx, a.host, a.port, a.timeout)
}

func (a *smtpProbeAdapter) DiscoverSchema(ctx context.Context) (Schema, error) {
	return Schema{}, fmt.Errorf("an SMTP server has no schema")
}

func (a *smtpProbeAdapter) ReadRecords(ctx context.Context, req ReadRequest) ([]Record, error) {
	return nil, fmt.Errorf("an SMTP server cannot be read")
}

func (a *smtpProbeAdapter) TransformRecords(ctx context.Context, records []Record, m map[string]string) ([]Record, error) {
	return transform(records, m), nil
}

func (a *smtpProbeAdapter) WriteRecords(ctx context.Context, records []Record) (WriteResult, error) {
	return WriteResult{}, ErrWriteNotSupported
}

func (a *smtpProbeAdapter) GetStatus(ctx context.Context) (Status, error) {
	if err := a.TestConnection(ctx); err != nil {
		return Status{Healthy: false, LastError: err.Error(), CheckedAt: time.Now()}, nil
	}
	return Status{Healthy: true, Message: "port reachable (credentials not verified)", CheckedAt: time.Now()}, nil
}

func (a *smtpProbeAdapter) Close() error { return nil }

// ---------------------------------------------------------------------------
// Database descriptor adapter
// ---------------------------------------------------------------------------

// databaseDescriptorAdapter reports configuration validity for a database source
// without opening a connection. The migration package owns connection handling so
// that a read-only session and its resource limits live in one place.
type databaseDescriptorAdapter struct {
	kind     string
	host     string
	database string
	user     string
}

func newDatabaseDescriptorAdapter(kind string, cfg map[string]any, secretsMap map[string]string) (Adapter, error) {
	return &databaseDescriptorAdapter{
		kind:     kind,
		host:     strOr(cfg["host"], ""),
		database: strOr(cfg["database"], ""),
		user:     secretsMap["username"],
	}, nil
}

func (a *databaseDescriptorAdapter) Kind() string { return a.kind }

func (a *databaseDescriptorAdapter) ValidateConfig(ctx context.Context) error {
	if a.kind != "sqlite" && a.host == "" {
		return fmt.Errorf("config.host is required for a %s source", a.kind)
	}
	if a.database == "" {
		return fmt.Errorf("config.database is required")
	}
	return nil
}

func (a *databaseDescriptorAdapter) TestConnection(ctx context.Context) error {
	// Connection testing for database sources runs through the migrations API,
	// which enforces read-only access and resource limits.
	return fmt.Errorf("database connection testing is performed through the migrations API")
}

func (a *databaseDescriptorAdapter) DiscoverSchema(ctx context.Context) (Schema, error) {
	return Schema{}, fmt.Errorf("use the migrations API to discover the schema of a database source")
}

func (a *databaseDescriptorAdapter) ReadRecords(ctx context.Context, req ReadRequest) ([]Record, error) {
	return nil, fmt.Errorf("use the migrations API to read from a database source")
}

func (a *databaseDescriptorAdapter) TransformRecords(ctx context.Context, records []Record, m map[string]string) ([]Record, error) {
	return transform(records, m), nil
}

func (a *databaseDescriptorAdapter) WriteRecords(ctx context.Context, records []Record) (WriteResult, error) {
	return WriteResult{}, ErrWriteNotSupported
}

func (a *databaseDescriptorAdapter) GetStatus(ctx context.Context) (Status, error) {
	return Status{Healthy: false, Message: "use the migrations API", CheckedAt: time.Now()}, nil
}

func (a *databaseDescriptorAdapter) Close() error { return nil }

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// transform renames fields according to a mapping and drops unmapped ones.
func transform(records []Record, m map[string]string) []Record {
	if len(m) == 0 {
		return records
	}
	out := make([]Record, 0, len(records))
	for _, rec := range records {
		next := Record{}
		for src, dst := range m {
			if v, ok := rec[src]; ok {
				next[dst] = v
			}
		}
		out = append(out, next)
	}
	return out
}

// decodeRecords accepts the common response shapes: a bare array, or an object
// with one of the usual collection keys.
func decodeRecords(raw json.RawMessage, limit int) ([]Record, error) {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var arr []Record
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		return capRecords(arr, limit), nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	for _, key := range []string{"data", "items", "records", "rows", "results"} {
		if v, ok := obj[key]; ok && strings.HasPrefix(strings.TrimSpace(string(v)), "[") {
			var arr []Record
			if err := json.Unmarshal(v, &arr); err != nil {
				return nil, err
			}
			return capRecords(arr, limit), nil
		}
	}
	return nil, fmt.Errorf("the response does not contain an array of records")
}

func capRecords(records []Record, limit int) []Record {
	if limit > 0 && len(records) > limit {
		return records[:limit]
	}
	return records
}

func strOr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func intOr(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return def
}
