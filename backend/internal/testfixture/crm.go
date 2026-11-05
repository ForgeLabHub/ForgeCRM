package testfixture

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/geff1778/forgecrm/internal/database"
)

// Account inserts one client organization and returns its id. Fields a test does
// not care about get deterministic defaults, so a caller names only what the
// assertion depends on.
func Account(t testing.TB, db *database.DB, orgID uuid.UUID, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO accounts (organization_id, name, email) VALUES ($1, $2, $3) RETURNING id`,
		orgID, name, slugify(name)+"@example.test").Scan(&id); err != nil {
		t.Fatalf("create account %q: %v", name, err)
	}
	return id
}

// Contact inserts one person, optionally attached to an account.
func Contact(t testing.TB, db *database.DB, orgID uuid.UUID, accountID *uuid.UUID, first, last string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO contacts (organization_id, account_id, first_name, last_name)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		orgID, accountID, first, last).Scan(&id); err != nil {
		t.Fatalf("create contact %q %q: %v", first, last, err)
	}
	return id
}

// Lead inserts one lead with a non-empty title, which the schema requires.
func Lead(t testing.TB, db *database.DB, orgID uuid.UUID, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO leads (organization_id, title, status) VALUES ($1, $2, 'new') RETURNING id`,
		orgID, title).Scan(&id); err != nil {
		t.Fatalf("create lead %q: %v", title, err)
	}
	return id
}

// Pipeline inserts a pipeline with the given ordered stages and returns the
// pipeline id and the stage ids in order. The first stage is the entry stage and
// the last is terminal-won, which is the shape every report and stage-change test
// assumes.
func Pipeline(t testing.TB, db *database.DB, orgID uuid.UUID, name string, stages ...string) (uuid.UUID, []uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if len(stages) == 0 {
		stages = []string{"New", "Won"}
	}

	var pipelineID uuid.UUID
	if err := db.Pool().QueryRow(ctx,
		`INSERT INTO pipelines (organization_id, name, is_default) VALUES ($1, $2, true) RETURNING id`,
		orgID, name).Scan(&pipelineID); err != nil {
		t.Fatalf("create pipeline %q: %v", name, err)
	}

	ids := make([]uuid.UUID, 0, len(stages))
	for i, s := range stages {
		var id uuid.UUID
		isWon := i == len(stages)-1
		if err := db.Pool().QueryRow(ctx,
			`INSERT INTO pipeline_stages (pipeline_id, organization_id, name, position, probability, is_won)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			pipelineID, orgID, s, i, stageProbability(i, len(stages)), isWon).Scan(&id); err != nil {
			t.Fatalf("create stage %q: %v", s, err)
		}
		ids = append(ids, id)
	}
	return pipelineID, ids
}

// Deal inserts one deal in the given stage.
func Deal(t testing.TB, db *database.DB, orgID, pipelineID, stageID uuid.UUID, title string, amount float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO deals (organization_id, pipeline_id, stage_id, title, amount, status)
		 VALUES ($1, $2, $3, $4, $5, 'open') RETURNING id`,
		orgID, pipelineID, stageID, title, amount).Scan(&id); err != nil {
		t.Fatalf("create deal %q: %v", title, err)
	}
	return id
}

// Task inserts one task.
func Task(t testing.TB, db *database.DB, orgID uuid.UUID, title string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO tasks (organization_id, title, status, priority) VALUES ($1, $2, 'open', 'normal') RETURNING id`,
		orgID, title).Scan(&id); err != nil {
		t.Fatalf("create task %q: %v", title, err)
	}
	return id
}

// Ticket inserts one support ticket.
func Ticket(t testing.TB, db *database.DB, orgID uuid.UUID, subject string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO tickets (organization_id, subject, status, priority, channel)
		 VALUES ($1, $2, 'new', 'normal', 'web') RETURNING id`,
		orgID, subject).Scan(&id); err != nil {
		t.Fatalf("create ticket %q: %v", subject, err)
	}
	return id
}

// Activity inserts one activity against an account.
func Activity(t testing.TB, db *database.DB, orgID, accountID uuid.UUID, kind, subject string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO activities (organization_id, type, subject, account_id, occurred_at)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		orgID, kind, subject, accountID, time.Now().UTC()).Scan(&id); err != nil {
		t.Fatalf("create activity %q: %v", subject, err)
	}
	return id
}

// Graph provisions a connected CRM graph for one tenant: an account, a contact,
// a pipeline, one deal, one task, one ticket and one activity. It returns the ids
// so a test can assert against the whole set — the shape Customer 360 and the
// reports read.
type Graph struct {
	Account  uuid.UUID
	Contact  uuid.UUID
	Pipeline uuid.UUID
	Stages   []uuid.UUID
	Deal     uuid.UUID
	Task     uuid.UUID
	Ticket   uuid.UUID
	Activity uuid.UUID
}

// Connected builds a Graph in the given organization.
func Connected(t testing.TB, db *database.DB, org *Org) *Graph {
	t.Helper()

	acct := Account(t, db, org.ID, "Connected Account")
	contact := Contact(t, db, org.ID, &acct, "Ada", "Lovelace")
	pipeline, stages := Pipeline(t, db, org.ID, "Connected Pipeline", "New", "Qualified", "Won")
	deal := Deal(t, db, org.ID, pipeline, stages[0], "Connected Deal", 10000)
	task := Task(t, db, org.ID, "Connected Task")
	ticket := Ticket(t, db, org.ID, "Connected Ticket")
	activity := Activity(t, db, org.ID, acct, "note", "Connected Activity")

	return &Graph{
		Account:  acct,
		Contact:  contact,
		Pipeline: pipeline,
		Stages:   stages,
		Deal:     deal,
		Task:     task,
		Ticket:   ticket,
		Activity: activity,
	}
}

// stageProbability gives a stage a plausible win probability: 10% at the start,
// rising to 100% at the terminal stage. A test that asserts on a weighted
// forecast needs a non-uniform spread, not every stage at zero.
func stageProbability(index, total int) int {
	if total <= 1 {
		return 100
	}
	return 10 + (90 * index / (total - 1))
}

// slugify reduces a display name to the character set an email local part and a
// database identifier accept.
func slugify(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' || r == '-' || r == '_':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "x"
	}
	return string(out)
}
