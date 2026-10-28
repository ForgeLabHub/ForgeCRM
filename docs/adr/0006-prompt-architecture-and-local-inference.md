# ADR 0006 — Prompt architecture and local inference

- **Status:** Accepted
- **Date:** 2026-10-28
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0001 (hybrid architecture), ADR 0005 (HITL agent execution model)
- **Scope:** `internal/ai/prompt.go`, `internal/ai/prompt_test.go`,
  `internal/ai/prompt_descriptors_test.go`, `internal/ai/mcp.go`
  (`PromptDescriptors`), `internal/api/ai_handlers.go`, `internal/config`

## Context

The AI layer had one prompt: a single constant string in the HTTP handler. It told
the model to treat record content as data, to use only tools, and not to claim
unperformed actions. Every one of those instructions was correct, and every one
was in the same place as the tool list, the output format and the per-request
context — or, worse, was absent from the parts that varied per request.

That shape has three concrete costs:

1. **It cannot be tested in parts.** A change to how lists are formatted and a
   change to what the agent may do arrive in the same diff, reviewed together, and
   there is no test that can fail on one without the other.
2. **The tool list drifts.** The tools the model is told about lived in prose. The
   tools the server authorises live in the registry. Two lists, one code path
   enforcing them, and nothing that fails when they disagree.
3. **Per-request data has no provenance.** A deal note and an operator instruction
   were both just text in the conversation. Nothing marked which could be believed.

Separately, the platform runs local models as a first-class option, and the
choice of inference engine had never been written down. "It seemed fast" is not a
decision anyone can act on later.

## Decision

### 1. Four prompt layers, assembled, not concatenated

The system prompt is assembled from four layers by `ai.AssembleSystemPrompt`:

| Layer | Content | Changes |
| --- | --- | --- |
| Role & scope | Who the agent is; that its authority is the caller's permissions and one organization; no answering CRM questions from general knowledge | With the code |
| Tool contract | How to choose a tool: ask rather than guess; explain a limitation rather than approximate; read before write; confirmation is a person's decision | With the code |
| Context injection | The request's data, each block labelled with its source and trust level | Every request |
| Output contract | Reply shape, and that a confirmation-required action must never be reported as done | With the code |

Layers 1, 2 and 4 are constants and are versioned as code. Layer 3 is built per
request. Keeping them separate is what allows each to be reviewed and tested
alone.

### 2. The tool contract is derived from the registry

The tool list is rendered from `ToolRegistry.PromptDescriptors()`, not written into
the prose. `RequiresConfirmation` mirrors the registry's `Dangerous` flag. A test
(`TestPromptDescriptorsComeFromTheRegistry`) asserts that a known read is not
advertised as needing confirmation and a known write is, so the prompt cannot
quietly describe a different tool set from the one the server enforces.

Rendering is order-stable (tools are sorted), so the same tool set produces an
identical prefix across requests. That is what makes prefix caching effective and
is why the sort exists rather than relying on map order.

### 3. Context is tagged with provenance, and cannot escape its block

Every `ContextBlock` carries a `Label`, a `Source` (e.g. `crm.accounts`,
`import:contacts.csv`) and a `Trust` level:

- `system` — operator-authored, the only class that may contain instructions;
- `internal` — data written by authenticated users of the organization (a comment
  is data, not a command channel);
- `external` — imported files, inbound email, webhook payloads, third-party tool
  output.

An unknown or unset trust level is downgraded to `external`, the least trusted,
rather than defaulting to trusted. The context section states in words that the
blocks are data and that no text inside one can grant a permission or authorize an
action. Each block is wrapped in a delimiter, and any delimiter inside the content
is neutralised, so a record cannot close its own block and continue as if it were
outside the data region (`TestContextCannotForgeItsWayOutOfTheDataBlock`).

**This tagging is a probabilistic filter, not a boundary.** The boundary is the
server: a tool call is authorized against the caller's permissions at execution
time regardless of what any text says, and a mutating tool needs a human decision
(ADR 0005). The tagging exists to make the model's job easier and to reduce the
rate of injection succeeding, not to be the thing that stops it.

### 4. Capability minimisation is the primary defence

The registry has eight CRM tools. There is no raw SQL, shell, filesystem or
unbounded export tool, by construction. The blast radius of a fully compromised
prompt is bounded by what the tools can do and by the permissions of the principal
they run as, not by the prompt's resilience. Every tool added is a new verb for an
attacker, which is why the default for an unrecognised tool name in `RiskTier` is
the most restrictive tier.

### 5. Local inference: engine is chosen by deployment, not by fashion

The same weights are served by different engines per environment:

| Environment | Engine | Why |
| --- | --- | --- |
| Development | Ollama | One-command install, single user |
| Staging | llama.cpp | CPU-friendly, full control over quantisation, minimal dependencies |
| Production | vLLM or SGLang | Batching and memory management that hold throughput under many concurrent users |

Ollama is a development engine. It is adequate for one user and degrades under
concurrent load, so it must not be the production default. The provider
abstraction (`ai.Provider`, with the Ollama and OpenAI-compatible implementations)
is the seam: the engine is configuration, not a code change.

Speed is mostly not a model choice. In order of effect: cache the stable prompt
prefix (this is why the tool list is order-stable), stream tokens to the UI rather
than waiting for a complete reply, do not call the model for work a SQL query can
do (a list of 100k deals is a query, not a completion), and batch bulk work rather
than issuing one call per record.

### 6. Degradation is explicit, and never a silent fallback to the cloud

If the configured local engine is unavailable, the platform enters a degraded mode
that says so. It does not fall back to a cloud provider: a silent fallback sends a
customer's data to a third party that was never configured to receive it. If the
configured cloud provider is unavailable, only read-only tools and template answers
remain. `ai.ErrDisabled` already models "AI off is a normal state"; degradation is
the same idea for "AI configured but unreachable".

## Consequences

- **Positive.** The stable layers are testable in isolation. The tool contract
  cannot drift from the registry. Provenance is explicit in the prompt, so the
  model can distinguish a record from an instruction. The engine choice is recorded
  and is configuration.
- **Negative.** Assembly is more code than a constant. The prompt is longer,
  which costs input tokens on every call — mitigated by prefix caching, which the
  order-stable tool list is designed for.
- **Not claimed.** Tagging does not make injection impossible; it lowers the hit
  rate and bounds the consequence. The claim is blast-radius containment plus
  server-side authorization, not prompt immunity.
- **Deferred.** The configuration schema for multiple named providers with routing
  (`tool_calling` / `reasoning` / `embeddings`) and per-tenant budget caps is
  designed here but only partially implemented: today a single provider is
  configured. The routing table is the next slice.
