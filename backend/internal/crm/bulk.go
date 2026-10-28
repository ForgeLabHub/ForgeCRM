package crm

import (
	"context"
	"errors"
	"strings"

	"github.com/geff1778/forgecrm/internal/httpx"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/google/uuid"
)

// Bulk archive.
//
// The per-record archive operations already exist and already do the right
// things: they scope the update to the caller's organization, refuse a record
// that is missing or already archived, emit an outbox event, and write an audit
// entry. Bulk archive reuses them one record at a time rather than issuing a
// single `UPDATE ... WHERE id = ANY($1)`.
//
// That choice is deliberate and costs some queries. A set-based update could not
// tell "archived" apart from "not found or not yours" per row without a
// RETURNING clause and extra bookkeeping, and it would have to emit one event and
// one audit entry per row anyway. Calling the existing method keeps a single
// definition of what archiving means, so the bulk path cannot drift from the
// single path. The caller gets a per-record result either way.

// MaxBulkArchiveIDs bounds one request. The limit exists so that a single call
// cannot hold a transaction open indefinitely or turn into an unbounded scan; a
// caller with more records sends more than one request.
const MaxBulkArchiveIDs = 200

// BulkArchiveResult reports what happened to each requested record.
//
// Failed is reported rather than silently dropped: a caller who asked to archive
// forty records and got thirty-three archived needs to know which seven were not,
// and whether that was because they were already archived or because they do not
// exist as far as this caller is concerned. The two are not distinguished in the
// response on purpose — see the comment on the reason strings.
type BulkArchiveResult struct {
	Archived []uuid.UUID
	Failed   []BulkArchiveFailure
}

// BulkArchiveFailure names one record that was not archived and why, in the
// caller's terms.
type BulkArchiveFailure struct {
	ID     uuid.UUID
	Reason string
}

// BulkArchiveInput is the request body for a bulk archive.
//
// IDs is validated for shape and size, not for existence: existence is checked
// per record during the operation, because a record can be archived by someone
// else between the validation and the update.
type BulkArchiveInput struct {
	IDs []uuid.UUID `json:"ids"`
}

// Validate enforces the shape rules the handler mirrors in the contract.
//
// It reports every problem it finds, keyed by field, so a client fixing a request
// sees all of them at once rather than one per round trip. The messages name the
// rule, not the value, so a rejected id is not echoed back.
func (in *BulkArchiveInput) Validate() error {
	details := map[string]string{}
	switch {
	case len(in.IDs) == 0:
		details["ids"] = "at least one id is required"
	case len(in.IDs) > MaxBulkArchiveIDs:
		details["ids"] = "at most " + itoa(MaxBulkArchiveIDs) + " ids per request"
	default:
		seen := make(map[uuid.UUID]struct{}, len(in.IDs))
		var nilSeen, duplicate bool
		for _, id := range in.IDs {
			if id == uuid.Nil {
				nilSeen = true
			}
			if _, dup := seen[id]; dup {
				duplicate = true
			}
			seen[id] = struct{}{}
		}
		var reasons []string
		if nilSeen {
			reasons = append(reasons, "must not contain the nil UUID")
		}
		if duplicate {
			reasons = append(reasons, "must not contain duplicates")
		}
		if len(reasons) > 0 {
			details["ids"] = strings.Join(reasons, "; ")
		}
	}
	if len(details) > 0 {
		return httpx.ValidationError(details)
	}
	return nil
}

// itoa avoids pulling strconv in for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// BulkArchive accounts, contacts or deals. The entity type selects which
// per-record operation is called, so the permission checked at the route is the
// permission of the underlying operation and nothing here can widen it.
//
// Every id is attempted. One record failing does not stop the others: a caller
// archiving a page of results should not have to retry the whole page because one
// row had already been archived.
func (s *Service) BulkArchive(ctx context.Context, p *rbac.Principal, entity string, in BulkArchiveInput) (BulkArchiveResult, error) {
	if err := in.Validate(); err != nil {
		return BulkArchiveResult{}, err
	}

	archive := s.bulkArchiver(entity)
	if archive == nil {
		// A programming error, not a client one: the route that reaches here exists
		// only for the entity types the switch below handles.
		return BulkArchiveResult{}, errors.New("bulk archive: unsupported entity type " + entity)
	}

	var out BulkArchiveResult
	for _, id := range in.IDs {
		if err := archive(ctx, p, id); err != nil {
			out.Failed = append(out.Failed, BulkArchiveFailure{ID: id, Reason: bulkArchiveReason(err)})
			continue
		}
		out.Archived = append(out.Archived, id)
	}
	return out, nil
}

// bulkArchiver returns the per-record archive function for an entity type.
//
// The three types are the ones with an archive operation today. Tasks, tickets
// and leads have an archived_at column but no archive operation, so they are
// absent here rather than silently half-supported.
func (s *Service) bulkArchiver(entity string) func(context.Context, *rbac.Principal, uuid.UUID) error {
	switch entity {
	case "accounts":
		return s.ArchiveAccount
	case "contacts":
		return s.ArchiveContact
	case "deals":
		return s.ArchiveDeal
	default:
		return nil
	}
}

// bulkArchiveReason turns a domain error into a phrase safe to return to the
// caller.
//
// A missing record and a record belonging to another organization both surface as
// ErrNotFound from the per-record operations, and both are reported the same way
// here. That is not sloppiness: distinguishing them would tell a caller that a
// record with a given id exists somewhere else, which is exactly the disclosure
// the not-found answer is there to prevent. The caller learns only what they can
// act on.
func bulkArchiveReason(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "not found or already archived"
	case errors.Is(err, rbac.ErrForbidden):
		return "forbidden"
	default:
		return "failed"
	}
}
