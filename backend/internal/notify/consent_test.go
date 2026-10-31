package notify

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/config"
)

// discardLogger keeps the transport's log lines out of the test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubGate records every question it is asked and answers from a scripted rule.
// The point of these tests is not that the policy is right — that is the consent
// package's own test suite — but that the transport asks, for every recipient, on
// the right channel and with the right purpose, before it does anything else.
type stubGate struct {
	allowed map[string]bool
	reason  string
	err     error
	asked   []ask
}

type ask struct {
	orgID   uuid.UUID
	address string
	purpose string
	channel string
}

func (g *stubGate) AllowSend(_ context.Context, orgID uuid.UUID, _ *uuid.UUID, address, purpose, channel string) (bool, string, error) {
	g.asked = append(g.asked, ask{orgID: orgID, address: address, purpose: purpose, channel: channel})
	if g.err != nil {
		return false, "", g.err
	}
	if allowed, ok := g.allowed[address]; ok {
		if allowed {
			return true, "granted", nil
		}
		return false, "no consent on record", nil
	}
	return false, g.reason, nil
}

// TestSendAsksConsentForEveryRecipient is the transport-level form of the
// multi-recipient bypass. The first address has a grant and the second does not;
// the transport must ask about the second rather than let the first answer for both.
func TestSendAsksConsentForEveryRecipient(t *testing.T) {
	gate := &stubGate{allowed: map[string]bool{"granted@test": true}}
	svc := NewEmailService(&config.EmailConfig{Enabled: false}, nil, discardLogger(), gate)

	err := svc.Send(context.Background(), Message{
		To:      []string{"granted@test", "stranger@test"},
		Subject: "x", Purpose: "marketing",
	})
	if !errors.Is(err, ErrConsentBlocked) {
		t.Fatalf("err = %v, want ErrConsentBlocked", err)
	}
	if len(gate.asked) != 2 {
		t.Fatalf("asked %d recipients, want 2: %+v", len(gate.asked), gate.asked)
	}
	if gate.asked[1].address != "stranger@test" {
		t.Fatalf("the second recipient was not asked about: %+v", gate.asked)
	}
}

// TestSendChecksConsentBeforeTheTransport pins the order. The transport is
// disabled, which would answer ErrDisabled if it were consulted first. Consent
// must answer first, so the caller is told the message is forbidden rather than
// that mail is not set up — the second answer implies the first would succeed once
// it were.
func TestSendChecksConsentBeforeTheTransport(t *testing.T) {
	gate := &stubGate{allowed: map[string]bool{}}
	svc := NewEmailService(&config.EmailConfig{Enabled: false}, nil, discardLogger(), gate)

	err := svc.Send(context.Background(), Message{
		To: []string{"nobody@test"}, Subject: "x", Purpose: "marketing",
	})
	if !errors.Is(err, ErrConsentBlocked) {
		t.Fatalf("err = %v, want the consent refusal, not the transport's", err)
	}
	if errors.Is(err, ErrDisabled) {
		t.Fatal("the transport answered before consent was asked")
	}
}

// TestSendDefaultsToTheStrictPurpose checks that an unset purpose is not a pass.
// The transport must ask about marketing, the category that requires a grant, so a
// caller that forgets to say what the message is about gets the safe rule.
func TestSendDefaultsToTheStrictPurpose(t *testing.T) {
	gate := &stubGate{allowed: map[string]bool{"someone@test": true}}
	svc := NewEmailService(&config.EmailConfig{Enabled: false}, nil, discardLogger(), gate)

	_ = svc.Send(context.Background(), Message{To: []string{"someone@test"}, Subject: "x"})

	if len(gate.asked) != 1 {
		t.Fatalf("asked %d times, want 1", len(gate.asked))
	}
	if gate.asked[0].purpose != "marketing" {
		t.Fatalf("purpose = %q, want the strict default marketing", gate.asked[0].purpose)
	}
	if gate.asked[0].channel != ChannelEmail {
		t.Fatalf("channel = %q, want %q", gate.asked[0].channel, ChannelEmail)
	}
}

// TestSendReportsTheGateError checks that a policy that cannot answer stops the
// send rather than being read as permission. A lookup failure is not a grant.
func TestSendReportsTheGateError(t *testing.T) {
	boom := errors.New("the consent store is unreachable")
	gate := &stubGate{err: boom}
	svc := NewEmailService(&config.EmailConfig{Enabled: false}, nil, discardLogger(), gate)

	err := svc.Send(context.Background(), Message{To: []string{"a@test"}, Subject: "x", Purpose: "marketing"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the gate's error", err)
	}
}

// TestChannelSpellingsMatchThePolicy pins the channel names the transports use to
// the ones the consent vocabulary uses. The two are separate on purpose — the
// transports must not import the policy — so a rename on one side would otherwise
// silently ask about a channel nobody has a decision for, which the policy reads
// as "no record" and, for a gated purpose, blocks. A drift that turns sends off
// must be caught here rather than in production.
func TestChannelSpellingsMatchThePolicy(t *testing.T) {
	// The policy's spellings, restated rather than imported, so this test fails when
	// either side moves instead of following the change.
	policyEmail, policyTelegram := "email", "telegram"
	if ChannelEmail != policyEmail {
		t.Errorf("notify ChannelEmail = %q, the policy says %q", ChannelEmail, policyEmail)
	}
	if ChannelTelegram != policyTelegram {
		t.Errorf("notify ChannelTelegram = %q, the policy says %q", ChannelTelegram, policyTelegram)
	}
	if DefaultPurpose != "marketing" {
		t.Errorf("DefaultPurpose = %q, want marketing", DefaultPurpose)
	}
	if PurposeTransactional != "transactional" {
		t.Errorf("PurposeTransactional = %q, want transactional", PurposeTransactional)
	}
}

// TestEnforceSkipsANilGate documents the one case where no policy is consulted:
// the extraction harness builds a transport with no gate and never sends. It keeps
// the nil path honest rather than accidental.
func TestEnforceSkipsANilGate(t *testing.T) {
	if err := enforce(context.Background(), nil, nil, nil, "a@test", "marketing", ChannelEmail); err != nil {
		t.Fatalf("a nil gate returned %v, want no policy consulted", err)
	}
}

// TestEnforceTreatsAMissingOrganizationAsNoTenant checks the account-level rule:
// with no organization the gate is asked with the zero id, so the policy sees no
// tenant and refuses the purposes that require a grant rather than allowing them.
func TestEnforceTreatsAMissingOrganizationAsNoTenant(t *testing.T) {
	gate := &stubGate{reason: "no tenant"}
	err := enforce(context.Background(), gate, nil, nil, "a@test", "marketing", ChannelEmail)
	if !errors.Is(err, ErrConsentBlocked) {
		t.Fatalf("enforce returned %v, want the gate's refusal", err)
	}
	if len(gate.asked) != 1 || gate.asked[0].orgID != uuid.Nil {
		t.Fatalf("gate asked %+v, want a single question with the zero organization", gate.asked)
	}
}
