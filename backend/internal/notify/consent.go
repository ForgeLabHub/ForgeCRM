package notify

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ConsentGate is the policy question a transport asks immediately before it sends
// to a person. It is declared here, in the transport package, rather than the
// transport importing the consent service, so the arrow points from policy to
// transport and not the other way: the transports stay ignorant of how a decision
// is reached, and the consent service stays ignorant of SMTP and Telegram.
//
// The gate is consulted inside Send, not by each caller. That placement is the
// point of the interface. A check a caller performs is a check the next caller can
// forget: the manual email endpoint enforced consent while the worker's queued-send
// handler did not, so the same message could leave the platform through the queue
// without a decision ever being asked. A transport that cannot send without asking
// makes that class of bypass impossible to reintroduce.
type ConsentGate interface {
	// AllowSend reports whether a message may be sent to address on channel for
	// purpose. When it refuses it returns the reason, which names the rule that was
	// applied so an operator can answer "why was this blocked". The implementation
	// records the refusal in the audit trail itself, so a blocked send cannot be
	// forgotten by a transport that only looked at the boolean.
	AllowSend(ctx context.Context, orgID uuid.UUID, actor *uuid.UUID, address, purpose, channel string) (allowed bool, reason string, err error)
}

// ErrConsentBlocked reports that the consent policy refused a send. It wraps the
// reason so a caller can surface it and can recognise the condition without
// matching on a message string.
var ErrConsentBlocked = errors.New("consent blocks this send")

// DefaultPurpose is the purpose assumed when a message does not state one.
//
// It is the strictest category on purpose. A caller that forgets to say what a
// message is about gets the rule that requires a recorded grant, rather than the
// transactional rule that does not, so an omission cannot silently authorise a
// marketing send.
const DefaultPurpose = "marketing"

// Channel names, in the spelling the consent policy uses. They are repeated here
// rather than imported from consent so the transports stay free of the policy
// package; the two spellings are pinned together by a test.
const (
	ChannelEmail    = "email"
	ChannelTelegram = "telegram"
)

// PurposeTransactional is the purpose of a message about the work itself — a task
// reminder, an account notification — as opposed to a marketing message. It is
// named here so the transports do not spell the category by hand.
const PurposeTransactional = "transactional"

// purposeOr returns the message's purpose, or the strict default when it is unset.
func purposeOr(purpose string) string {
	if purpose == "" {
		return DefaultPurpose
	}
	return purpose
}

// enforce runs the gate for one address. A nil gate means no policy is configured,
// which is the extraction harness that never sends; a real deployment always
// injects one.
func enforce(ctx context.Context, gate ConsentGate, orgID *uuid.UUID, actor *uuid.UUID, address, purpose, channel string) error {
	if gate == nil {
		return nil
	}
	// An unset organization is passed through as the zero id rather than skipped. The
	// policy then sees no tenant, finds no record, and refuses the purposes that
	// require a grant — which is the correct answer, and the one that keeps "the
	// caller forgot to say which tenant this is" from becoming an authorisation.
	tenant := uuid.Nil
	if orgID != nil {
		tenant = *orgID
	}
	allowed, reason, err := gate.AllowSend(ctx, tenant, actor, address, purposeOr(purpose), channel)
	if err != nil {
		return err
	}
	if !allowed {
		if reason == "" {
			reason = "no reason given"
		}
		return &blockedError{reason: reason}
	}
	return nil
}

// blockedError carries the policy's reason and unwraps to ErrConsentBlocked, so
// errors.Is(err, ErrConsentBlocked) is true while the message still explains why.
type blockedError struct{ reason string }

func (e *blockedError) Error() string { return ErrConsentBlocked.Error() + ": " + e.reason }
func (e *blockedError) Unwrap() error { return ErrConsentBlocked }

// Reason returns the policy's explanation for a blocked send.
func (e *blockedError) Reason() string { return e.reason }
