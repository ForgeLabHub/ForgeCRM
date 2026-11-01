package consent

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestPurposesAndChannelsAreClosed pins the vocabularies. A purpose or channel
// outside the set would be a decision nothing can look up, so it is refused at
// write time rather than stored and then ignored by every policy check.
func TestPurposesAndChannelsAreClosed(t *testing.T) {
	for _, ok := range []Purpose{PurposeMarketing, PurposeTransactional, PurposeProductUpdates, PurposeResearch} {
		if !ok.Valid() {
			t.Errorf("Purpose(%q).Valid() = false, want true", ok)
		}
	}
	for _, bad := range []Purpose{"", "Marketing", "advertising"} {
		if bad.Valid() {
			t.Errorf("Purpose(%q).Valid() = true, want false", bad)
		}
	}
	for _, ok := range []Channel{ChannelEmail, ChannelTelegram, ChannelWebhook} {
		if !ok.Valid() {
			t.Errorf("Channel(%q).Valid() = false, want true", ok)
		}
	}
	for _, bad := range []Channel{"", "sms", "Email"} {
		if bad.Valid() {
			t.Errorf("Channel(%q).Valid() = true, want false", bad)
		}
	}
	for _, ok := range []State{StateGranted, StateDenied, StateWithdrawn} {
		if !ok.Valid() {
			t.Errorf("State(%q).Valid() = false, want true", ok)
		}
	}
	for _, bad := range []State{"", "granted ", "opted_in"} {
		if bad.Valid() {
			t.Errorf("State(%q).Valid() = true, want false", bad)
		}
	}
}

// TestRequiresGrantIsThePurposeDefault is the rule that decides what silence
// means. Marketing, product updates and research need a recorded grant;
// transactional mail does not, because a password reset must not be blocked by the
// absence of a marketing consent.
func TestRequiresGrantIsThePurposeDefault(t *testing.T) {
	if !PurposeMarketing.RequiresGrant() {
		t.Error("marketing must require a recorded grant")
	}
	if !PurposeProductUpdates.RequiresGrant() {
		t.Error("product updates must require a recorded grant")
	}
	if !PurposeResearch.RequiresGrant() {
		t.Error("research must require a recorded grant")
	}
	if PurposeTransactional.RequiresGrant() {
		t.Error("transactional mail must not require a recorded grant")
	}
}

// TestEveryPurposeIsClassifiedByGrantRequirement is the guard that keeps silence
// from becoming permission. RequiresGrant is derived from grantRequiredPurposes,
// so a purpose added to the vocabulary but to neither set would be treated as
// exempt — the permissive answer — with no test to notice. Every supported purpose
// must be in exactly one set: gated or explicitly exempt.
func TestEveryPurposeIsClassifiedByGrantRequirement(t *testing.T) {
	for p := range supportedPurposes {
		_, gated := grantRequiredPurposes[p]
		_, exempt := purposesExemptFromGrant[p]
		switch {
		case gated && exempt:
			t.Errorf("purpose %q is both gated and exempt; the two sets must not overlap", p)
		case !gated && !exempt:
			t.Errorf("purpose %q is in neither set, so RequiresGrant treats it as exempt; "+
				"classify it as gated or exempt", p)
		case gated != p.RequiresGrant():
			t.Errorf("purpose %q: RequiresGrant() = %v, classification says %v", p, p.RequiresGrant(), gated)
		}
	}
	for p := range grantRequiredPurposes {
		if !p.Valid() {
			t.Errorf("grantRequiredPurposes names %q, which is not a supported purpose", p)
		}
	}
	for p := range purposesExemptFromGrant {
		if !p.Valid() {
			t.Errorf("purposesExemptFromGrant names %q, which is not a supported purpose", p)
		}
	}
}

// TestValidateRefusesAnAmbiguousSubject is the check that keeps "which person is
// this about" from being a guess: a record naming both a contact and an address, or
// neither, is refused.
func TestValidateRefusesAnAmbiguousSubject(t *testing.T) {
	id := uuid.New()
	both := Input{ContactID: &id, SubjectEmail: "a@example.test", Purpose: "marketing", Channel: "email", State: "granted"}
	var ve *ValidationError
	if err := validate(both); !errors.As(err, &ve) {
		t.Fatalf("validate(both subjects) = %v, want a ValidationError", err)
	} else if _, ok := ve.Fields["subject"]; !ok {
		t.Errorf("error does not name the subject field: %v", ve.Fields)
	}

	neither := Input{Purpose: "marketing", Channel: "email", State: "granted"}
	if err := validate(neither); !errors.As(err, &ve) {
		t.Fatalf("validate(no subject) = %v, want a ValidationError", err)
	}
}

// TestValidateNamesEveryOffendingField checks that one call reports all the
// problems rather than the first, so a client can fix a form in one pass.
func TestValidateNamesEveryOffendingField(t *testing.T) {
	err := validate(Input{SubjectEmail: "a@example.test", Purpose: "nope", Channel: "nope", State: "nope"})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("validate = %v, want a ValidationError", err)
	}
	for _, field := range []string{"purpose", "channel", "state"} {
		if _, ok := ve.Fields[field]; !ok {
			t.Errorf("error does not name %q: %v", field, ve.Fields)
		}
	}
}

func TestValidateAcceptsAWellFormedDecision(t *testing.T) {
	id := uuid.New()
	if err := validate(Input{ContactID: &id, Purpose: "marketing", Channel: "email", State: "granted"}); err != nil {
		t.Fatalf("validate = %v, want nil", err)
	}
	if err := validate(Input{SubjectEmail: "a@example.test", Purpose: "transactional", Channel: "email", State: "denied"}); err != nil {
		t.Fatalf("validate = %v, want nil", err)
	}
}

// TestEmailIsNormalised keeps a decision recorded for one spelling of an address
// findable under another. Without it an unsubscribe for "Ada@Example.com" would not
// match a record made for "ada@example.com", and the send would go out.
func TestEmailIsNormalised(t *testing.T) {
	s := SubjectForEmail("  Ada@Example.COM ")
	if s.Email != "ada@example.com" {
		t.Errorf("SubjectForEmail normalised to %q, want %q", s.Email, "ada@example.com")
	}
	if got := subjectKey(s); got != "e:ada@example.com" {
		t.Errorf("subjectKey = %q, want %q", got, "e:ada@example.com")
	}
}

func TestSubjectKeyDistinguishesAContactFromAnAddress(t *testing.T) {
	id := uuid.New()
	if got, want := subjectKey(SubjectForContact(id)), "c:"+id.String(); got != want {
		t.Errorf("subjectKey(contact) = %q, want %q", got, want)
	}
}

func TestZeroSubjectIsDetected(t *testing.T) {
	if !(Subject{}).IsZero() {
		t.Error("an empty subject should be zero")
	}
	if (Subject{Email: "  "}).IsZero() != true {
		t.Error("a whitespace-only address should be zero")
	}
	id := uuid.New()
	if (SubjectForContact(id)).IsZero() {
		t.Error("a contact subject should not be zero")
	}
	if (SubjectForEmail("a@example.test")).IsZero() {
		t.Error("an address subject should not be zero")
	}
}
