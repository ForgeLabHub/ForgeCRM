package crm

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/httpx"
)

// The validators are the one part of the CRM service that runs before any database
// access, and they are the boundary an attacker's payload hits first. They had no
// tests before this slice; these are the first.
//
// Every case asserts on the *field key* the error carries, not just that an error
// occurred: the API returns details keyed by field, so a message under the wrong
// key is a wrong answer even when a validation did happen.

// detailsOf extracts the field->message map from a validation error, or fails.
func detailsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	if err == nil {
		t.Fatal("expected a validation error, got nil")
	}
	apiErr, ok := err.(*httpx.APIError)
	if !ok {
		t.Fatalf("expected an *httpx.APIError, got %T: %v", err, err)
	}
	if apiErr.Code != httpx.CodeValidation {
		t.Fatalf("expected code %q, got %q", httpx.CodeValidation, apiErr.Code)
	}
	return apiErr.Details
}

func TestAccountInputValidate(t *testing.T) {
	// A minimal valid account.
	if err := (&AccountInput{Name: "Acme"}).Validate(); err != nil {
		t.Fatalf("a valid account was rejected: %v", err)
	}

	// The name is trimmed before the emptiness check, so whitespace is not a name.
	err := (&AccountInput{Name: "   "}).Validate()
	if _, ok := detailsOf(t, err)["name"]; !ok {
		t.Errorf("a whitespace-only name was not rejected: %v", err)
	}

	// A name at the boundary is accepted; one over it is not.
	long := strings.Repeat("a", 301)
	if err := (&AccountInput{Name: long}).Validate(); err == nil {
		t.Error("a 301-character name was accepted")
	}
	if err := (&AccountInput{Name: strings.Repeat("a", 300)}).Validate(); err != nil {
		t.Errorf("a 300-character name was rejected: %v", err)
	}

	// A malformed email is rejected; an empty one is treated as absent, not invalid.
	bad := "not-an-email"
	if err := (&AccountInput{Name: "Acme", Email: &bad}).Validate(); err == nil {
		t.Error("a malformed email was accepted")
	}
	empty := ""
	if err := (&AccountInput{Name: "Acme", Email: &empty}).Validate(); err != nil {
		t.Errorf("an empty email was rejected: %v", err)
	}

	// Negative numbers are refused.
	neg := -1
	if err := (&AccountInput{Name: "Acme", EmployeeCount: &neg}).Validate(); err == nil {
		t.Error("a negative employee count was accepted")
	}
	negF := -0.01
	if err := (&AccountInput{Name: "Acme", AnnualRevenue: &negF}).Validate(); err == nil {
		t.Error("a negative annual revenue was accepted")
	}

	// The external pair is all-or-nothing: one without the other is a mistake, and
	// the validator must not silently accept a half-set identity.
	src := "hubspot"
	id := "123"
	if err := (&AccountInput{Name: "Acme", ExternalSource: &src}).Validate(); err == nil {
		t.Error("an external_source without an external_id was accepted")
	}
	if err := (&AccountInput{Name: "Acme", ExternalID: &id}).Validate(); err == nil {
		t.Error("an external_id without an external_source was accepted")
	}
	if err := (&AccountInput{Name: "Acme", ExternalSource: &src, ExternalID: &id}).Validate(); err != nil {
		t.Errorf("a complete external pair was rejected: %v", err)
	}
}

func TestDealInputValidate(t *testing.T) {
	base := func() *DealInput {
		return &DealInput{PipelineID: uuid.New(), StageID: uuid.New(), Title: "Renewal"}
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("a valid deal was rejected: %v", err)
	}

	// The currency defaults to USD, so a blank one is not an error but the field
	// must end up a 3-letter code.
	d := base()
	if err := d.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if d.Currency != "USD" {
		t.Errorf("currency default = %q, want USD", d.Currency)
	}

	// A pipeline and a stage are required; the nil UUID is not a valid one.
	missing := &DealInput{Title: "x", StageID: uuid.New()}
	if _, ok := detailsOf(t, missing.Validate())["pipeline_id"]; !ok {
		t.Error("a deal with no pipeline was not rejected on pipeline_id")
	}

	// Discount is a percentage; the ends are inclusive and just outside is refused.
	for _, pct := range []float64{-0.1, 100.1} {
		d := base()
		d.DiscountPercent = pct
		if err := d.Validate(); err == nil {
			t.Errorf("discount %v was accepted", pct)
		}
	}
	for _, pct := range []float64{0, 100} {
		d := base()
		d.DiscountPercent = pct
		if err := d.Validate(); err != nil {
			t.Errorf("discount %v was rejected: %v", pct, err)
		}
	}

	// The close date must be an ISO date when present, and blank is treated as absent.
	badDate := "next tuesday"
	if err := baseWith(base(), func(d *DealInput) { d.ExpectedCloseDate = &badDate }).Validate(); err == nil {
		t.Error("a non-ISO close date was accepted")
	}
	blank := ""
	if err := baseWith(base(), func(d *DealInput) { d.ExpectedCloseDate = &blank }).Validate(); err != nil {
		t.Errorf("a blank close date was rejected: %v", err)
	}
	good := "2026-03-01"
	if err := baseWith(base(), func(d *DealInput) { d.ExpectedCloseDate = &good }).Validate(); err != nil {
		t.Errorf("a valid ISO close date was rejected: %v", err)
	}

	// A bad currency length is caught even after the default.
	d = base()
	d.Currency = "US"
	if err := d.Validate(); err == nil {
		t.Error("a 2-letter currency was accepted")
	}
}

func baseWith(d *DealInput, f func(*DealInput)) *DealInput {
	f(d)
	return d
}

func TestContactInputValidate(t *testing.T) {
	// A contact needs a name or an email; a first name alone is enough.
	first := "Ada"
	if err := (&ContactInput{FirstName: &first}).Validate(); err != nil {
		t.Errorf("a first-name-only contact was rejected: %v", err)
	}
	email := "ada@example.com"
	if err := (&ContactInput{Email: &email}).Validate(); err != nil {
		t.Errorf("an email-only contact was rejected: %v", err)
	}
	if err := (&ContactInput{}).Validate(); err == nil {
		t.Error("a contact with neither a name nor an email was accepted")
	}
	// A whitespace-only name is not a name.
	blank := "  "
	if err := (&ContactInput{FirstName: &blank}).Validate(); err == nil {
		t.Error("a whitespace-only name was accepted")
	}
	// A malformed email is still rejected even when a name is present.
	bad := "nope"
	if err := (&ContactInput{FirstName: &first, Email: &bad}).Validate(); err == nil {
		t.Error("a malformed email was accepted")
	}
}

func TestTaskInputValidateDefaultsAndEnums(t *testing.T) {
	in := &TaskInput{Title: "Call back"}
	if err := in.Validate(); err != nil {
		t.Fatalf("a valid task was rejected: %v", err)
	}
	if in.Status != "open" || in.Priority != "normal" {
		t.Errorf("defaults = %q/%q, want open/normal", in.Status, in.Priority)
	}

	for _, status := range []string{"open", "in_progress", "completed", "cancelled"} {
		if err := (&TaskInput{Title: "x", Status: status}).Validate(); err != nil {
			t.Errorf("status %q was rejected: %v", status, err)
		}
	}
	if err := (&TaskInput{Title: "x", Status: "done"}).Validate(); err == nil {
		t.Error("an unknown task status was accepted")
	}
	if err := (&TaskInput{Title: "x", Priority: "asap"}).Validate(); err == nil {
		t.Error("an unknown priority was accepted")
	}
	if err := (&TaskInput{Title: "  "}).Validate(); err == nil {
		t.Error("a whitespace-only title was accepted")
	}
}

func TestTicketInputValidateDefaultsAndEnums(t *testing.T) {
	in := &TicketInput{Subject: "Broken login"}
	if err := in.Validate(); err != nil {
		t.Fatalf("a valid ticket was rejected: %v", err)
	}
	if in.Status != "new" || in.Priority != "normal" || in.Channel != "web" {
		t.Errorf("defaults = %q/%q/%q, want new/normal/web", in.Status, in.Priority, in.Channel)
	}
	if err := (&TicketInput{Subject: "x", Channel: "carrier-pigeon"}).Validate(); err == nil {
		t.Error("an unknown channel was accepted")
	}
	if err := (&TicketInput{Subject: "x", Status: "escalated"}).Validate(); err == nil {
		t.Error("an unknown ticket status was accepted")
	}
	if err := (&TicketInput{}).Validate(); err == nil {
		t.Error("a ticket with no subject was accepted")
	}
}

func TestActivityInputRequiresATarget(t *testing.T) {
	// An activity must point at something; a free-floating note is not addressable.
	if err := (&ActivityInput{Type: "note"}).Validate(); err == nil {
		t.Error("an activity with no target was accepted")
	}
	deal := uuid.New()
	if err := (&ActivityInput{Type: "note", DealID: &deal}).Validate(); err != nil {
		t.Errorf("an activity on a deal was rejected: %v", err)
	}
	if err := (&ActivityInput{Type: "sneeze", DealID: &deal}).Validate(); err == nil {
		t.Error("an unknown activity type was accepted")
	}
}

func TestBulkArchiveInputValidate(t *testing.T) {
	// The cap is the shape rule the handler mirrors.
	ids := make([]uuid.UUID, MaxBulkArchiveIDs)
	for i := range ids {
		ids[i] = uuid.New()
	}
	if err := (&BulkArchiveInput{IDs: ids}).Validate(); err != nil {
		t.Errorf("a request at the cap was rejected: %v", err)
	}
	if err := (&BulkArchiveInput{IDs: append(ids, uuid.New())}).Validate(); err == nil {
		t.Error("a request over the cap was accepted")
	}
	if err := (&BulkArchiveInput{}).Validate(); err == nil {
		t.Error("an empty request was accepted")
	}
	// A nil UUID and a duplicate are each rejected; both at once are reported
	// together, because the validator collects reasons rather than stopping at one.
	id := uuid.New()
	if err := (&BulkArchiveInput{IDs: []uuid.UUID{id, id}}).Validate(); err == nil {
		t.Error("a duplicate id was accepted")
	}
	if err := (&BulkArchiveInput{IDs: []uuid.UUID{uuid.Nil}}).Validate(); err == nil {
		t.Error("a nil UUID was accepted")
	}
	err := (&BulkArchiveInput{IDs: []uuid.UUID{uuid.Nil, id, id}}).Validate()
	details := detailsOf(t, err)
	if !strings.Contains(details["ids"], "nil") || !strings.Contains(details["ids"], "duplicate") {
		t.Errorf("both reasons were not reported: %q", details["ids"])
	}
}

func TestJSONMapValueAndScan(t *testing.T) {
	// A nil map persists as an empty object, not SQL NULL, so a reader never has to
	// handle both an absent key and a null document.
	v, err := JSONMap(nil).Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	if s, ok := v.(string); !ok || s != "{}" {
		t.Errorf("nil JSONMap Value() = %v, want {}", v)
	}

	// Scan accepts the bytes and string forms the driver may hand back, and treats
	// nil/empty as an empty map.
	var m JSONMap
	if err := m.Scan([]byte(`{"a":1}`)); err != nil || m["a"] != float64(1) {
		t.Errorf("scan bytes = %v (%v)", m, err)
	}
	var m2 JSONMap
	if err := m2.Scan(`{"b":2}`); err != nil || m2["b"] != float64(2) {
		t.Errorf("scan string = %v (%v)", m2, err)
	}
	var m3 JSONMap
	if err := m3.Scan(nil); err != nil || len(m3) != 0 {
		t.Errorf("scan nil = %v (%v)", m3, err)
	}
	var m4 JSONMap
	if err := m4.Scan("not json"); err == nil {
		t.Error("scan of invalid JSON did not fail")
	}
	var m5 JSONMap
	if err := m5.Scan(42); err == nil {
		t.Error("scan of an unsupported type did not fail")
	}
}

func TestLooksLikeEmail(t *testing.T) {
	valid := []string{"a@b.co", "ada.lovelace@example.com", "x+y@sub.example.org"}
	for _, s := range valid {
		if !looksLikeEmail(s) {
			t.Errorf("%q was rejected as an email", s)
		}
	}
	invalid := []string{"", "@", "a@", "@b.co", "a@b", "a b@c.co", "a@b c.co", "a\n@b.co"}
	for _, s := range invalid {
		if looksLikeEmail(s) {
			t.Errorf("%q was accepted as an email", s)
		}
	}
}
