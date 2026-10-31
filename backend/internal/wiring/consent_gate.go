package wiring

import (
	"context"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/consent"
	"github.com/geff1778/forgecrm/internal/notify"
)

// consentGate adapts the consent policy to the narrow question the transports ask.
//
// It lives in the composition root because it is the one place allowed to know both
// the transport package and the policy package. Putting it here rather than inside
// either keeps their import arrows pointing inward: the transports declare the
// interface they need, the policy implements the decision, and only the wiring
// joins them. That is what lets the notify module still extract on its own, with no
// dependency on consent, as scripts/extract-test/notify verifies.
type consentGate struct {
	svc *consent.Service
}

// NewConsentGate builds the transport-facing view of the consent policy.
func NewConsentGate(svc *consent.Service) notify.ConsentGate {
	return &consentGate{svc: svc}
}

// AllowSend answers whether a message may go to an address on a channel.
//
// The address is the subject, not a contact id: consent is recorded against the
// person, and the person is reachable whether or not they are already a contact.
// A purpose or channel the platform does not know is refused here rather than
// passed on, so a caller cannot widen the rule by naming a category that has none.
func (g *consentGate) AllowSend(ctx context.Context, orgID uuid.UUID, actor *uuid.UUID, address, purpose, channel string) (bool, string, error) {
	p := consent.Purpose(purpose)
	if !p.Valid() {
		return false, "the purpose " + purpose + " is not a category the platform knows", nil
	}
	if !consent.Channel(channel).Valid() {
		return false, "the channel " + channel + " is not a channel the platform knows", nil
	}
	// No organization means account-level mail: a password reset or a security notice
	// sent to the address owner, with no tenant in context to hold a decision. Such a
	// message is allowed only for a purpose that does not require a recorded grant.
	// That is what keeps account recovery working — it must not depend on a marketing
	// consent — while stopping a caller from dropping the organization to turn a
	// marketing send into an ungated one.
	if orgID == uuid.Nil {
		if p.RequiresGrant() {
			return false, "a " + purpose + " message may not be sent without an organization in context", nil
		}
		return true, "account-level mail for a purpose that does not require a recorded grant", nil
	}
	// Enforce, not Decide: it records the refusal in the audit trail in the same call
	// that reaches the decision, so a blocked send is visible rather than silent.
	d, err := g.svc.Enforce(ctx, orgID, actor, consent.SubjectForEmail(address), p, consent.Channel(channel))
	if err != nil {
		return false, "", err
	}
	return d.Allowed, d.Reason, nil
}
