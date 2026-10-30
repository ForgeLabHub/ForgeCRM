//go:build integration

package testsupport

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/rbac"
)

// consentFixture is a consent service with two users in one organization and a
// member of a second organization, for the tenant-isolation check.
type consentFixture struct {
	db    *database.DB
	svc   *consent.Service
	orgA  uuid.UUID
	orgB  uuid.UUID
	userA uuid.UUID
	userB uuid.UUID
	other uuid.UUID
	prinA *rbac.Principal
	prinB *rbac.Principal
	// prinOther carries the same consent permissions but acts in a second
	// organization: the realistic shape of a cross-tenant attempt.
	prinOther *rbac.Principal
}

func newConsentFixture(t *testing.T) *consentFixture {
	t.Helper()
	db := connectOrSkip(t)
	freshSchema(t, db)

	orgA := insertOrganization(t, db, "consent-a")
	orgB := insertOrganization(t, db, "consent-b")
	userA := seedMember(t, db, orgA, "sales_manager")
	userB := seedMember(t, db, orgA, "sales_representative")
	other := seedMember(t, db, orgB, "sales_manager")

	perms := []rbac.Permission{rbac.PermConsentRead, rbac.PermConsentManage}
	return &consentFixture{
		db:        db,
		svc:       consent.NewService(db, audit.New(db)),
		orgA:      orgA,
		orgB:      orgB,
		userA:     userA,
		userB:     userB,
		other:     other,
		prinA:     rbac.NewPrincipal(userA, "a@example.test", "A", orgA, perms),
		prinB:     rbac.NewPrincipal(userB, "b@example.test", "B", orgA, perms),
		prinOther: rbac.NewPrincipal(other, "c@example.test", "C", orgB, perms),
	}
}

// insertContact creates a contact in an organization for the subject-key test.
func insertContact(t *testing.T, db *database.DB, orgID uuid.UUID, first, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO contacts (organization_id, first_name, last_name, email)
                 VALUES ($1, $2, 'Person', $3) RETURNING id`,
		orgID, first, email).Scan(&id); err != nil {
		t.Fatalf("insert contact %q: %v", first, err)
	}
	return id
}

// TestConsentDecideIsDenyByDefaultForMarketing is the core rule: no record means a
// marketing send is blocked, not allowed. "We emailed them because nobody said no"
// is the failure this feature exists to prevent.
func TestConsentDecideIsDenyByDefaultForMarketing(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	d, err := f.svc.Decide(ctx, f.orgA, consent.SubjectForEmail("stranger@example.test"),
		consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Allowed {
		t.Fatal("a marketing send with no consent record must be blocked")
	}
	if d.State != "" {
		t.Errorf("a missing record should report no state, got %q", d.State)
	}
}

// TestConsentDecideAllowsTransactionalWithoutARecord pins the one deliberate
// exception: a password reset must not be blocked by the absence of a marketing
// consent, or account recovery breaks.
func TestConsentDecideAllowsTransactionalWithoutARecord(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	d, err := f.svc.Decide(ctx, f.orgA, consent.SubjectForEmail("stranger@example.test"),
		consent.PurposeTransactional, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("transactional mail must not require a recorded grant, got %q", d.Reason)
	}
}

// TestConsentGrantThenDenyThenWithdrawTracksHistory checks the revision chain: each
// change of mind supersedes the previous decision without overwriting it, and only
// the latest one is in force.
func TestConsentGrantThenDenyThenWithdrawTracksHistory(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	subject := consent.SubjectForEmail("person@example.test")

	grant, err := f.svc.Record(ctx, f.prinA, consent.Input{
		SubjectEmail: "person@example.test", Purpose: "marketing", Channel: "email",
		State: "granted", Basis: "signup form", Source: "web form",
	})
	if err != nil {
		t.Fatalf("record grant: %v", err)
	}
	if grant.Revision != 1 {
		t.Errorf("first decision revision = %d, want 1", grant.Revision)
	}
	if !grant.Current {
		t.Error("a freshly recorded decision should be current")
	}

	d, err := f.svc.Decide(ctx, f.orgA, subject, consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide after grant: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("a granted decision must allow the send, got %q", d.Reason)
	}

	withdraw, err := f.svc.Withdraw(ctx, f.prinA, subject, consent.PurposeMarketing, consent.ChannelEmail, "unsubscribe link")
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if withdraw.Revision != 2 {
		t.Errorf("withdrawal revision = %d, want 2", withdraw.Revision)
	}

	d, err = f.svc.Decide(ctx, f.orgA, subject, consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide after withdrawal: %v", err)
	}
	if d.Allowed {
		t.Fatal("a withdrawn decision must block the send")
	}

	// The history keeps both revisions, and the earlier one is no longer current.
	history, err := f.svc.History(ctx, f.prinA, subject, consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history has %d revisions, want 2", len(history))
	}
	if !history[0].Current || history[1].Current {
		t.Error("only the latest revision should be current")
	}
	if history[1].SupersededBy == nil {
		t.Error("the superseded revision should point at its successor")
	}

	// Current lists only the revision in force.
	current, err := f.svc.Current(ctx, f.prinA, subject)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if len(current) != 1 || current[0].State != "withdrawn" {
		t.Fatalf("current = %+v, want a single withdrawn record", current)
	}
}

// TestConsentIsScopedToOnePurposeAndChannel checks that the decision key is the
// triple, not the person: agreeing to be emailed about marketing does not authorise
// a Telegram message, nor an email about something else.
func TestConsentIsScopedToOnePurposeAndChannel(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	subject := consent.SubjectForEmail("person@example.test")

	if _, err := f.svc.Record(ctx, f.prinA, consent.Input{
		SubjectEmail: "person@example.test", Purpose: "marketing", Channel: "email", State: "granted",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	cases := []struct {
		name    string
		purpose consent.Purpose
		channel consent.Channel
		want    bool
	}{
		{"same purpose and channel", consent.PurposeMarketing, consent.ChannelEmail, true},
		{"different channel", consent.PurposeMarketing, consent.ChannelTelegram, false},
		{"different purpose", consent.PurposeProductUpdates, consent.ChannelEmail, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := f.svc.Decide(ctx, f.orgA, subject, tc.purpose, tc.channel)
			if err != nil {
				t.Fatalf("decide: %v", err)
			}
			if d.Allowed != tc.want {
				t.Errorf("allowed = %v, want %v (reason: %s)", d.Allowed, tc.want, d.Reason)
			}
		})
	}
}

// TestConsentSubjectEmailIsCaseInsensitive checks that an unsubscribe recorded for
// one spelling of an address is found under another, so a decision cannot be missed
// because of capitalisation.
func TestConsentSubjectEmailIsCaseInsensitive(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Record(ctx, f.prinA, consent.Input{
		SubjectEmail: "Ada@Example.COM", Purpose: "marketing", Channel: "email", State: "granted",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	d, err := f.svc.Decide(ctx, f.orgA, consent.SubjectForEmail("ada@example.com"),
		consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if !d.Allowed {
		t.Fatalf("a decision recorded for a differently-cased address must be found, got %q", d.Reason)
	}
}

// TestConsentExpiryEndsAGrant checks that a grant with an expiry stops allowing the
// send once it has passed, without any further write.
func TestConsentExpiryEndsAGrant(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	subject := consent.SubjectForEmail("person@example.test")

	past := time.Now().Add(-time.Hour)
	decided := time.Now().Add(-2 * time.Hour)
	if _, err := f.svc.Record(ctx, f.prinA, consent.Input{
		SubjectEmail: "person@example.test", Purpose: "marketing", Channel: "email",
		State: "granted", ExpiresAt: &past, DecidedAt: &decided,
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	d, err := f.svc.Decide(ctx, f.orgA, subject, consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if d.Allowed {
		t.Fatal("an expired grant must not allow the send")
	}
	if d.State != "granted" {
		t.Errorf("state = %q, want granted (the record still exists, it has expired)", d.State)
	}
}

// TestConsentIsTenantScoped is the isolation check: a decision recorded in one
// organization is invisible in another, so a consent given to one tenant cannot be
// read — or relied on — by a different one.
func TestConsentIsTenantScoped(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()
	email := "shared@example.test"

	if _, err := f.svc.Record(ctx, f.prinA, consent.Input{
		SubjectEmail: email, Purpose: "marketing", Channel: "email", State: "granted",
	}); err != nil {
		t.Fatalf("record in org A: %v", err)
	}

	// The other tenant's decision is absent, so the send is blocked there.
	d, err := f.svc.Decide(ctx, f.orgB, consent.SubjectForEmail(email),
		consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide in org B: %v", err)
	}
	if d.Allowed {
		t.Fatal("a consent recorded in one organization must not allow a send in another")
	}

	// And the record is not listed for a principal acting in the other tenant.
	listed, err := f.svc.Current(ctx, f.prinOther, consent.SubjectForEmail(email))
	if err != nil {
		t.Fatalf("current in org B: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("org B sees %d records, want 0", len(listed))
	}
}

// TestConsentEnforceAuditsABlock checks that a refused send leaves a trail. An
// invisible refusal is the state this feature exists to end.
func TestConsentEnforceAuditsABlock(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	d, err := f.svc.Enforce(ctx, f.orgA, &f.userA, consent.SubjectForEmail("stranger@example.test"),
		consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("enforce: %v", err)
	}
	if d.Allowed {
		t.Fatal("enforce must block a marketing send with no record")
	}

	var count int
	if err := f.db.Pool().QueryRow(ctx, `
SELECT count(*) FROM audit_log
WHERE organization_id = $1 AND action = 'consent.send_blocked'`, f.orgA).Scan(&count); err != nil {
		t.Fatalf("count block entries: %v", err)
	}
	if count != 1 {
		t.Fatalf("blocked-send entries = %d, want 1", count)
	}
}

// TestConsentEnforceDoesNotAuditAnAllowedSend checks the other half: an allowed send
// is not written to the trail, so the count of block entries means what it says.
func TestConsentEnforceDoesNotAuditAnAllowedSend(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	if _, err := f.svc.Record(ctx, f.prinA, consent.Input{
		SubjectEmail: "person@example.test", Purpose: "transactional", Channel: "email", State: "granted",
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := f.svc.Enforce(ctx, f.orgA, &f.userA, consent.SubjectForEmail("person@example.test"),
		consent.PurposeTransactional, consent.ChannelEmail); err != nil {
		t.Fatalf("enforce: %v", err)
	}

	var count int
	if err := f.db.Pool().QueryRow(ctx, `
SELECT count(*) FROM audit_log
WHERE organization_id = $1 AND action = 'consent.send_blocked'`, f.orgA).Scan(&count); err != nil {
		t.Fatalf("count block entries: %v", err)
	}
	if count != 0 {
		t.Fatalf("blocked-send entries = %d, want 0", count)
	}
}

// TestConsentRecordRefusesAnAmbiguousSubject checks that a record naming both a
// contact and an address is refused rather than silently preferring one.
func TestConsentRecordRefusesAnAmbiguousSubject(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	contactID := uuid.New()
	_, err := f.svc.Record(ctx, f.prinA, consent.Input{
		ContactID: &contactID, SubjectEmail: "both@example.test",
		Purpose: "marketing", Channel: "email", State: "granted",
	})
	var ve *consent.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("record with both subjects = %v, want a ValidationError", err)
	}
}

// TestConsentDecisionByContactIDIsFoundByEmailAndViceVersa documents the current
// boundary: a decision keyed by contact id and one keyed by the same person's
// address are separate records. The test pins that so the behaviour is deliberate
// rather than a surprise.
func TestConsentDecisionByContactIDIsSeparateFromAnAddress(t *testing.T) {
	f := newConsentFixture(t)
	ctx := context.Background()

	contactID := insertContact(t, f.db, f.orgA, "Ada", "ada@example.test")
	if _, err := f.svc.Record(ctx, f.prinA, consent.Input{
		ContactID: &contactID, Purpose: "marketing", Channel: "email", State: "granted",
	}); err != nil {
		t.Fatalf("record by contact: %v", err)
	}

	byContact, err := f.svc.Decide(ctx, f.orgA, consent.SubjectForContact(contactID),
		consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide by contact: %v", err)
	}
	if !byContact.Allowed {
		t.Fatalf("a decision recorded by contact must allow that contact, got %q", byContact.Reason)
	}

	byEmail, err := f.svc.Decide(ctx, f.orgA, consent.SubjectForEmail("ada@example.test"),
		consent.PurposeMarketing, consent.ChannelEmail)
	if err != nil {
		t.Fatalf("decide by email: %v", err)
	}
	if byEmail.Allowed {
		t.Fatal("a contact-keyed decision is not the same as an address-keyed one; " +
			"the send by bare address must not be allowed")
	}
}
