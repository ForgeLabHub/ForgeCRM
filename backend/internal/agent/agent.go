// Package agent implements the human-in-the-loop execution model described in
// ADR 0005.
//
// The package owns one invariant: an action that requires approval cannot be
// performed until a signed human decision exists for the exact arguments the
// operator saw. It enforces that invariant in the database, not in a prompt, so
// it holds regardless of what the model says or which code path a caller takes.
package agent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/database"
)

// RiskTier classifies what an action does and therefore what it requires before
// it may run.
//
// The tier is a property of the tool, assigned in Go, and copied onto the proposed
// action. Nothing the model produces can lower it: a tool declared destructive
// stays destructive no matter what the agent's context claims.
type RiskTier string

const (
	// TierSafe is a read-only or otherwise harmless operation. It runs immediately.
	TierSafe RiskTier = "safe"
	// TierConfirm mutates state and requires a human decision.
	TierConfirm RiskTier = "confirm"
	// TierDestructive is irreversible or wide-reaching and requires a human
	// decision plus a re-authentication factor.
	TierDestructive RiskTier = "destructive"
)

// Valid reports whether the tier is one the schema accepts.
func (t RiskTier) Valid() bool {
	return t == TierSafe || t == TierConfirm || t == TierDestructive
}

// RequiresApproval reports whether the tier must be gated behind a decision.
func (t RiskTier) RequiresApproval() bool { return t == TierConfirm || t == TierDestructive }

// Run statuses.
const (
	RunRunning         = "running"
	RunWaitingApproval = "waiting_approval"
	RunCompleted       = "completed"
	RunFailed          = "failed"
	RunCancelled       = "cancelled"
)

// Action statuses.
const (
	ActionProposed = "proposed"
	ActionApproved = "approved"
	ActionRejected = "rejected"
	ActionExpired  = "expired"
	ActionExecuted = "executed"
	ActionFailed   = "failed"
)

// Domain errors. They are sentinel values so handlers can map them to HTTP status
// codes without inspecting strings.
var (
	// ErrActionChanged is returned when the arguments hash the operator approved
	// no longer matches the stored action. It closes the window between the page
	// rendering the diff and the operator clicking approve.
	ErrActionChanged = errors.New("the action changed since it was displayed")
	// ErrAlreadyDecided is returned when a decision already exists for an action.
	ErrAlreadyDecided = errors.New("the action has already been decided")
	// ErrExpired is returned when a proposal's deadline has passed.
	ErrExpired = errors.New("the approval request has expired")
	// ErrBadSignature is returned when a decision's HMAC does not verify.
	ErrBadSignature = errors.New("the approval signature is not valid")
	// ErrBudgetExceeded is returned when a run has spent its token or action budget.
	ErrBudgetExceeded = errors.New("the run has exhausted its budget")
	// ErrRunNotActive is returned when an action is proposed against a run that is
	// not in a state where proposals are accepted.
	ErrRunNotActive = errors.New("the run is not accepting actions")
	// ErrToolDenied is returned when a tool is on the never-invoke list.
	ErrToolDenied = errors.New("the tool may not be invoked by an agent")
	// ErrStepUpRequired is returned when a destructive action is decided from a
	// session that has not presented a second factor. The tier exists precisely for
	// the actions where a single compromised session must not be enough.
	ErrStepUpRequired = errors.New("a second authentication factor is required to decide this action")
)

// approvalKey is the purpose-derived HMAC key for decision signatures. It mirrors
// the Keyring's DeriveCSRF/WebhookSign pattern: the platform secret is never used
// raw, and this purpose cannot collide with another.
func approvalKey(platformSecret string) []byte {
	mac := hmac.New(sha256.New, []byte(platformSecret))
	mac.Write([]byte("forgecrm:approval:v1"))
	return mac.Sum(nil)
}

// CanonicalArguments renders arguments to a stable byte string for hashing.
//
// Go's encoding/json sorts map keys, so two equal maps marshal identically. That
// is what makes the hash meaningful: it identifies the arguments, not the order
// they arrived in.
func CanonicalArguments(args map[string]any) ([]byte, error) {
	if args == nil {
		args = map[string]any{}
	}
	return json.Marshal(args)
}

// ArgumentsHash is the SHA-256 of the canonical arguments, hex-encoded with a
// scheme prefix so a future algorithm change is distinguishable.
func ArgumentsHash(args map[string]any) (string, error) {
	canonical, err := CanonicalArguments(args)
	if err != nil {
		return "", fmt.Errorf("canonicalise arguments: %w", err)
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// decisionSignature computes the HMAC over the canonical decision tuple.
//
// The tuple is built from length-prefixed fields so that two different tuples
// cannot collide by concatenation — a classic signature bug where
// ("ab","c") and ("a","bc") produce the same string.
func decisionSignature(key []byte, actionID, orgID uuid.UUID, argumentsHash string,
	decidedBy uuid.UUID, decision string, decidedAt time.Time) string {

	var b strings.Builder
	write := func(field string) {
		fmt.Fprintf(&b, "%d:%s", len(field), field)
	}
	write(actionID.String())
	write(orgID.String())
	write(argumentsHash)
	write(decidedBy.String())
	write(decision)
	write(decidedAt.UTC().Format(time.RFC3339Nano))

	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(b.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// Run is one working cycle of an agent acting on behalf of a user.
type Run struct {
	ID             uuid.UUID  `json:"id"`
	OrganizationID uuid.UUID  `json:"organization_id"`
	UserID         uuid.UUID  `json:"user_id"`
	Goal           string     `json:"goal"`
	Status         string     `json:"status"`
	BudgetTokens   int        `json:"budget_tokens"`
	TokensUsed     int        `json:"tokens_used"`
	ActionCap      int        `json:"action_cap"`
	ActionsUsed    int        `json:"actions_used"`
	Error          *string    `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

// Action is a proposed operation awaiting a decision, or a record of one that has
// been decided and possibly executed.
type Action struct {
	ID             uuid.UUID       `json:"id"`
	RunID          uuid.UUID       `json:"run_id"`
	OrganizationID uuid.UUID       `json:"organization_id"`
	ToolName       string          `json:"tool_name"`
	Arguments      json.RawMessage `json:"arguments"`
	ArgumentsHash  string          `json:"arguments_hash"`
	RiskTier       RiskTier        `json:"risk_tier"`
	Summary        string          `json:"summary"`
	Status         string          `json:"status"`
	IdempotencyKey string          `json:"idempotency_key"`
	Result         json.RawMessage `json:"result,omitempty"`
	Error          *string         `json:"error,omitempty"`
	ExpiresAt      time.Time       `json:"expires_at"`
	ProposedAt     time.Time       `json:"proposed_at"`
	DecidedAt      *time.Time      `json:"decided_at,omitempty"`
	ExecutedAt     *time.Time      `json:"executed_at,omitempty"`
}

// Decider is the human making a decision.
type Decider struct {
	UserID         uuid.UUID
	OrganizationID uuid.UUID
	// StepUpVerified reports whether the deciding session has presented a second
	// factor. It is consulted only for destructive actions: a destructive decision
	// from a session that has not presented one is refused. It is a property of the
	// request, resolved by the caller from the session, never from the request body.
	StepUpVerified bool
}

// Proposal is the request to propose an action.
type Proposal struct {
	RunID          uuid.UUID
	OrganizationID uuid.UUID
	ToolName       string
	Arguments      map[string]any
	RiskTier       RiskTier
	// Summary is written by the caller from the tool definition, never from model
	// prose, so the operator's diff cannot be a lie.
	Summary        string
	IdempotencyKey string
	// TTL bounds the wait. Zero means the configured default.
	TTL time.Duration
}

// ExecutionFunc performs an approved action's effect. It receives the action and
// the decider whose approval authorized it, and must be idempotent under the
// action's idempotency key.
type ExecutionFunc func(ctx context.Context, action *Action, decider Decider) (any, error)

// Executor resolves a tool name to its execution. It is the registry the agent
// runtime consults at execution time.
type Executor interface {
	// Execution returns the function that performs the tool's effect, or an error
	// if the tool is unknown or not executable.
	Execution(toolName string) (ExecutionFunc, error)
}

// Service is the HITL state machine.
type Service struct {
	db         *database.DB
	audit      *audit.Logger
	executor   Executor
	key        []byte
	defaultTTL time.Duration
	// deny is the set of tool names the agent may never invoke, regardless of any
	// approval. It is checked at propose time so a denied tool cannot even enter
	// the queue.
	deny map[string]struct{}
}

// Config configures the service.
type Config struct {
	// PlatformSecret derives the decision-signing key. It must be non-empty; an
	// empty secret would make every signature forgeable.
	PlatformSecret string
	// DeniedTools are tool names the agent may never invoke.
	DeniedTools []string
	// DefaultTTL bounds how long a proposal waits. Defaults to 24h.
	DefaultTTL time.Duration
}

// NewService builds the HITL service.
func NewService(db *database.DB, auditLog *audit.Logger, executor Executor, cfg Config) (*Service, error) {
	if cfg.PlatformSecret == "" {
		return nil, errors.New("agent: platform secret must not be empty")
	}
	deny := make(map[string]struct{}, len(cfg.DeniedTools))
	for _, name := range cfg.DeniedTools {
		deny[name] = struct{}{}
	}
	ttl := cfg.DefaultTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Service{
		db:         db,
		audit:      auditLog,
		executor:   executor,
		key:        approvalKey(cfg.PlatformSecret),
		defaultTTL: ttl,
		deny:       deny,
	}, nil
}

// Denied reports whether a tool is on the never-invoke list.
func (s *Service) Denied(toolName string) bool {
	_, ok := s.deny[toolName]
	return ok
}

// DeniedTools returns the deny list in sorted order, for documentation and tests.
func (s *Service) DeniedTools() []string {
	out := make([]string, 0, len(s.deny))
	for name := range s.deny {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// rowNotFound reports whether an error is a "no rows" from a guarded statement.
func rowNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
