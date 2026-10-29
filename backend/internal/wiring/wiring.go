// Package wiring constructs the application's services from a configuration and
// a database handle.
//
// It exists as a package rather than a function in package main so that the API
// binary, the worker binary and the test suite all assemble the same object
// graph. A test that builds its own dependencies can drift from production: it
// can pass while the real startup path is broken, or miss a missing dependency
// entirely. Assembling services here means an API test exercises the same code
// that runs in the deployed binary.
package wiring

import (
	"fmt"
	"log/slog"

	"github.com/geff1778/forgecrm/internal/agent"
	"github.com/geff1778/forgecrm/internal/ai"
	"github.com/geff1778/forgecrm/internal/audit"
	"github.com/geff1778/forgecrm/internal/authn"
	"github.com/geff1778/forgecrm/internal/config"
	"github.com/geff1778/forgecrm/internal/crm"
	"github.com/geff1778/forgecrm/internal/database"
	"github.com/geff1778/forgecrm/internal/events"
	"github.com/geff1778/forgecrm/internal/importx"
	"github.com/geff1778/forgecrm/internal/integrations"
	"github.com/geff1778/forgecrm/internal/jobs"
	"github.com/geff1778/forgecrm/internal/migratex"
	"github.com/geff1778/forgecrm/internal/notify"
	"github.com/geff1778/forgecrm/internal/outbox"
	"github.com/geff1778/forgecrm/internal/rbac"
	"github.com/geff1778/forgecrm/internal/secrets"
	"github.com/geff1778/forgecrm/internal/storage"
	"github.com/geff1778/forgecrm/internal/workflow"
)

// Services bundles the constructed dependencies.
type Services struct {
	Audit        *audit.Logger
	Auth         *authn.Service
	RBAC         *rbac.Service
	CRM          *crm.Service
	Jobs         *jobs.Queue
	Outbox       *outbox.Store
	Imports      *importx.Service
	Migrations   *migratex.Service
	Integrations *integrations.Hub
	Events       *events.Gateway
	Dispatcher   *events.Dispatcher
	Email        *notify.EmailService
	Telegram     *notify.TelegramService
	AI           *ai.Service
	Tools        *ai.ToolRegistry
	// MCP is the protocol transport over Tools.
	MCP *ai.MCPServer
	// Agent is the human-in-the-loop execution state machine (ADR 0005). It is
	// wired with an executor built from Tools, so an approved action runs through
	// exactly the same permission-checked tool path as a direct call.
	Agent *agent.Service
	// Storage is the optional S3-compatible attachment store. It is nil-able: a
	// deployment without storage still constructs the service, which reports itself
	// off, so the diagnostics endpoint and the document routes can both ask.
	Storage *storage.Service
	// Workflows is the automation engine. It is built from the same crm.Service and
	// rbac.Service the API uses, so a workflow action is authorized exactly like the
	// equivalent API call.
	Workflows *workflow.Engine
}

// Build constructs every service from the given configuration and database.
//
// The order is not arbitrary. The audit logger comes first because almost every
// other service writes to it; the CRM service is built before the import service
// because imports write through it; and the tool registry is built last because
// it depends on the CRM service.
func Build(cfg *config.Config, db *database.DB, keyring *secrets.Keyring, log *slog.Logger) (*Services, error) {
	auditLog := audit.New(db)
	rbacSvc := rbac.NewService(db)
	crmSvc := crm.NewService(db, auditLog, outbox.NewStore(db))
	queue := jobs.NewQueue(db, log)

	imports, err := importx.NewService(db, cfg, auditLog, outbox.NewStore(db), crmSvc, queue, cfg.ImportStorageDir)
	if err != nil {
		return nil, fmt.Errorf("initialise the import service: %w", err)
	}

	tools := ai.NewToolRegistry(crmSvc, auditLog)

	// The agent runtime executes approved actions through the tool registry, so it
	// needs the registry and the RBAC service to rebuild the decider's principal at
	// execution time.
	executor := ai.NewAgentExecutor(db, rbacSvc, tools)
	agentSvc, err := agent.NewService(db, auditLog, executor, agent.Config{
		PlatformSecret: cfg.SecretKey,
		DeniedTools:    ai.DeniedToolNames(),
	})
	if err != nil {
		return nil, fmt.Errorf("initialise the agent service: %w", err)
	}

	storageSvc, err := buildStorage(cfg, db, auditLog, log)
	if err != nil {
		return nil, fmt.Errorf("initialise object storage: %w", err)
	}

	return &Services{
		Storage:      storageSvc,
		Audit:        auditLog,
		Auth:         authn.NewService(db, cfg, keyring, auditLog, rbacSvc),
		RBAC:         rbacSvc,
		CRM:          crmSvc,
		Jobs:         queue,
		Outbox:       outbox.NewStore(db),
		Imports:      imports,
		Migrations:   migratex.NewService(db, auditLog, keyring),
		Integrations: integrations.NewHub(db, auditLog, keyring),
		Events:       events.NewGateway(db, auditLog),
		Dispatcher:   events.NewDispatcher(db, auditLog),
		Email:        notify.NewEmailService(&cfg.Email, db, log),
		Telegram:     notify.NewTelegramService(&cfg.Telegram, db, log, auditLog, keyring),
		AI:           ai.NewService(&cfg.AI, db, auditLog, crmSvc, log),
		Tools:        tools,
		MCP:          ai.NewMCPServer(tools),
		Agent:        agentSvc,
		Workflows: workflow.NewEngine(workflow.Config{
			DB:     db,
			CRM:    crmSvc,
			RBAC:   rbacSvc,
			Audit:  auditLog,
			Outbox: outbox.NewStore(db),
			Log:    log,
		}),
	}, nil
}
