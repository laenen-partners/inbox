# inbox

A task-driven inbox for GenAI-first applications, backed by the [entity store](https://github.com/laenen-partners/entitystore). Inbox items are units of work to be resolved by a human or AI agent — approvals, reviews, data collection, compliance checks, or any async decision point in a workflow.

The inbox is the coordination layer between AI agents, humans, and workflows. AI agents create items, triage them, resolve what they can, and escalate what they can't. Humans see the same items, with the same API, in the same queue. The system doesn't distinguish between the two — it just tracks who did what.

## Use cases

### AI agent triage and routing

An AI extraction pipeline processes incoming documents. When confidence is high, it resolves automatically. When confidence is low or a conflict is detected, it creates an inbox item for human review — tagged with the right team, priority, and context.

```
Document arrives → AI extracts data → confidence < threshold
    → inbox.Create(type:review, team:ops, payload: extraction result)
    → human reviews, corrects, responds
    → workflow continues with corrected data
```

### Human-in-the-loop for compliance (KYC/AML)

Eligibility rules flag requirements that need human judgment — PEP screening, source of funds, sanctions checks. The inbox bridges the gap between automated evaluation and manual decision-making.

```
Prodcat evaluates eligibility → requirement pending (manual_review)
    → inbox.Create(type:review, team:compliance, payload: requirement details)
    → compliance officer claims, reviews screening report, approves/rejects
    → workflow re-evaluates with the decision, activates subscription
```

### Customer data collection

When a customer application is missing data (ID documents, address proof, employment details), the inbox creates items assigned to the customer. The customer — or an RM acting on their behalf — provides the data.

```
Eligibility evaluation → missing passport
    → inbox.Create(type:input_required, assignee:customer:CUST-123)
    → customer uploads document via action form
    → workflow re-evaluates eligibility
```

### RM override on behalf of client

A relationship manager can claim and respond to any item assigned to their clients. The event trail captures the delegation — who acted, on whose behalf, and why.

```
Customer can't complete online → calls the bank
    → RM claims the item, acts on behalf of client
    → event trail: actor=user:rm:sarah, on_behalf_of=customer:CUST-123
```

### AI agent auto-resolution

An AI agent watches the inbox via semantic search or tag subscriptions. When it finds items it can handle (e.g. straightforward verifications, known patterns), it responds automatically. Items it can't handle stay for humans.

```
Agent polls: ib.ListByTags(["status:open", "type:verification"])
    → for each item: evaluate confidence
    → high confidence: ib.On(itemID, "agent:auto-verify").
          WithEvent(&VerificationCompleted{...}).
          Respond("approve", "Auto-verified").
          TransitionTo(StatusCompleted).Apply()
    → low confidence: skip (human picks it up)
```

### Multi-step onboarding journey

A single workflow creates multiple inbox items — some for the customer, some for compliance, some for ops. Tags link them all to the same workflow for correlation. The workflow completes when all items reach terminal states.

```
Onboarding workflow starts
    → inbox.Create(type:action, assignee:customer, "Verify email")
    → inbox.Create(type:input_required, assignee:customer, "Upload ID")
    → inbox.Create(type:review, team:compliance, "PEP screening")
    → workflow polls: ib.ListByTags(["workflow:onboarding-456", "status:open"])
    → all completed → activate subscription
```

### Escalation chains

Ops discovers an item needs specialist review. They escalate to compliance, which escalates to legal. Each escalation updates the team tags and records a typed event — the full chain is visible in the audit trail.

```
Ops claims → reviews → escalates to compliance
    → ib.Escalate(itemID, "user:ops:marco", "ops", "compliance", "Needs EDD")
    → compliance claims → escalates to legal
    → full trail: ops → compliance → legal, with reasons at each step
```

## Architecture

```
                        +-----------------------+
                        |      Workflows        |
                        |  (DBOS, cron, etc.)   |
                        +----------+------------+
                                   |
                          Create / Complete
                          UpdatePayload
                                   |
                                   v
+------------+    Respond    +----------+    callback     +------------+
|   Human    | -----------> |          | --------------> |  Workflow   |
|   (RM,     |    Claim     |  Inbox   |                 |  Engine    |
|  client)   |    Comment   |          | <-- Create      +------------+
+------------+              +----+-----+
                                 |
+------------+    Respond        |
|  AI Agent  | ----------->     |
|  (triage,  |    WithEvent     |
|   KYC bot) |    Comment       |
+------------+                  |
                                v
                   +------------------------+
                   |     Entity Store       |
                   |  (PostgreSQL + JSONB)  |
                   |                        |
                   |  +------------------+  |
                   |  | entities         |  |
                   |  |  - id            |  |
                   |  |  - entity_type   |  |
                   |  |  - data (JSONB)  |  |
                   |  |  - tags[]        |  |
                   |  |  - embedding     |  |
                   |  +------------------+  |
                   +------------------------+
                                |
            +-------------------+-------------------+
            |                   |                   |
      Tags (GIN)         Tokens (GIN)        Embeddings
      routing &          fuzzy text          semantic
      filtering          search              search
```

### Data flow

```
Workflow/Agent/Human
        |
        |  inbox.Create(meta)
        v
  +-- Inbox Item (entity) ----------------------------------+
  |                                                         |
  |  title: "PEP screening review — Ahmed K."               |
  |  status: open                                           |
  |  payload_type: eligibility.v1.EligibilityReviewPayload  |
  |  payload: { subscriptionId: "SUB-042", ... }            |
  |                                                         |
  |  tags: [type:review, team:compliance, priority:high,    |
  |         status:open, workflow:onboarding-456]            |
  |                                                         |
  |  events:                                                |
  |    [inbox.v1.ItemCreated]  workflow:onboarding-456       |
  |    [inbox.v1.ItemClaimed]  user:compliance:fatima        |
  |    [inbox.v1.CommentAppended] "Checked screening..."    |
  |    [inbox.v1.ItemResponded]  approve                    |
  |    [inbox.v1.ItemCompleted]  workflow:onboarding-456     |
  +------ ----- ----- ----- ----- ----- ----- ----- -------+
```

### Item lifecycle

```
              Create
                |
                v
  +--------+  Claim   +---------+
  |  open  | -------> | claimed |
  +---+----+          +----+----+
      ^                    |
      |     Release        |
      +--------------------+
      |                    |
      |    Respond (no status change)
      |                    |
      v                    v
  +---------+        +-----------+
  | expired |        | completed |
  +---------+        +-----------+
      ^
      |                +-----------+
      +-- Expire       | cancelled |
                       +-----------+
                            ^
                            |
                       Cancel (from any
                        non-terminal)
```

## Concepts

### Items are entities

Every inbox item is an entity in the entity store (type `inbox.v1.Item`). This means items get tags, embeddings, token search, provenance tracking, and all other entity store features for free. There is no separate `inbox_items` table.

### Tags as routing

Items are routed and filtered entirely via tags — free-form `key:value` strings stored in the entity store's GIN-indexed tags column. Adding a new priority level, team, item type, or link requires zero schema changes.

| Tag | Purpose |
|---|---|
| `type:approval` | Item kind |
| `priority:urgent` | Urgency level |
| `team:compliance` | Owning team |
| `assignee:user:sarah` | Assigned human or agent |
| `source:workflow:inv-123` | What created this item |
| `workflow:onboarding-456` | Parent workflow |
| `ref:invoice:789` | Link to a related entity |
| `status:open` | Mirrored from the status field |

### Typed payloads

Items carry a `payload_type` (fully qualified proto message name) and a `payload` (the serialized proto as JSON). The inbox stores and delivers payloads without interpretation — payload schemas are owned by the domain that creates the item.

This gives you:
- **Type discrimination** without parsing the payload (`data->>'payload_type'`)
- **Schema versioning** via proto's backwards-compatible evolution
- **Type-safe deserialization** via the proto registry
- **Analytics** aggregation across the entire inbox by payload type

### Typed events

Every operation on an item produces a typed event. The `Event.Type` field is the fully qualified proto message name (e.g. `inbox.v1.ItemClaimed`), derived automatically from the proto message — never set manually.

Custom domain events are emitted via `WithEvent(proto.Message)` on the Op builder.

Standard event types:

| Type | Emitted by |
|---|---|
| `inbox.v1.ItemCreated` | `Create()` |
| `inbox.v1.ItemClaimed` | `Claim()` |
| `inbox.v1.ItemReleased` | `Release()` |
| `inbox.v1.ItemResponded` | `Respond()` |
| `inbox.v1.ItemCompleted` | `Complete()` |
| `inbox.v1.ItemCancelled` | `Cancel()` |
| `inbox.v1.ItemExpired` | `Expire()` |
| `inbox.v1.CommentAppended` | `Comment()` |
| `inbox.v1.ItemEscalated` | `Escalate()` |
| `inbox.v1.ItemReassigned` | `Reassign()` |
| `inbox.v1.TagsChanged` | `Tag()` / `Untag()` |
| `inbox.v1.PayloadUpdated` | `UpdatePayload()` |

### State is not derived from events

The inbox is **not** event-sourced. The item's current state — `status`, `payload`, `tags` — is the source of truth, stored directly in the entity store as a mutable document. You never replay events to reconstruct state.

| To answer... | Read... |
|---|---|
| What form should I render? | `item.Payload` + `item.PayloadType` |
| Who is this assigned to? | `item.Tags` (`assignee:...`) |
| Is this item still open? | `item.Status` |
| What happened on this item? | `item.Events` |
| Who approved it and when? | `item.Events` (audit trail) |
| Average time-to-response? | `item.Events` (analytics) |

Events are an **audit log**, not a projection source.

### Workflow integration

The inbox is not coupled to any workflow runtime. Workflows integrate via a callback tag (`callback:<address>`) and a pluggable `Dispatcher` interface. When someone responds to an item, the inbox fires the callback. The workflow owns the lifecycle from there — it decides whether to complete, update, or create follow-up items.

### Actors

Humans, AI agents, and system processes all interact with items through the same API. The `actor` field on every operation follows a convention:
- `user:<id>` — human user
- `agent:<name>` — AI agent
- `workflow:<id>` — workflow engine
- `system` — background process

## Usage

### Setup

```go
import (
    "github.com/laenen-partners/entitystore"
    "github.com/laenen-partners/inbox"
)

pool, _ := pgxpool.New(ctx, connString)
es, _ := entitystore.New(entitystore.WithPgStore(pool))

// Basic inbox.
ib := inbox.New(es)

// With callback dispatcher for workflow integration.
ib := inbox.New(es, inbox.WithDispatcher(myWebhookDispatcher))
```

### Creating items

```go
item, err := ib.Create(ctx, inbox.Meta{
    Title:       "Approve vendor invoice #1234",
    Description: "Invoice from Acme Corp for $5,000. Matches PO-789.",
    Actor:       "workflow:invoice-processing-456",
    Tags: []string{
        "type:approval",
        "team:finance",
        "priority:normal",
        "ref:invoice:INV-1234",
        "workflow:invoice-processing-456",
    },
})
```

### Creating items with typed payloads

```go
typeURL, data, _ := inbox.PackPayload(&eligibilityv1.EligibilityReviewPayload{
    SubscriptionId:  "SUB-2026-0042",
    ProductName:     "Current Account — AED",
    RequirementName: "pep_screening_clear",
    FailureMode:     "manual_review",
    CustomerId:      "CUST-1234",
})

item, err := ib.Create(ctx, inbox.Meta{
    Title:       "PEP screening review — Ahmed K.",
    Description: "Customer flagged as PEP during onboarding.",
    PayloadType: typeURL,
    Payload:     data,
    Actor:       "workflow:onboarding-456",
    Tags:        []string{"type:review", "team:compliance", "priority:high"},
})

// Or use the SetPayload convenience:
meta := inbox.Meta{Title: "...", Description: "..."}
inbox.SetPayload(&meta, &myProtoPayload)
item, err := ib.Create(ctx, meta)
```

### Idempotency

Set `IdempotencyKey` to prevent duplicate item creation on workflow retries:

```go
item, err := ib.Create(ctx, inbox.Meta{
    Title:          "Upload Emirates ID",
    Actor:          "workflow:onboarding-456",
    IdempotencyKey: "workflow:onboarding-456:valid_passport",
    Tags:           []string{"type:input_required", "assignee:customer:CUST-1234"},
})
```

### Item lifecycle

```go
item, err := ib.Claim(ctx, itemID, "user:sarah")
item, err := ib.Release(ctx, itemID, "user:sarah")

// Respond does NOT complete — workflow decides.
item, err := ib.Respond(ctx, itemID, inbox.Response{
    Actor:   "user:sarah",
    Action:  "approve",
    Comment: "Verified against PO, amounts match.",
})

item, err := ib.Complete(ctx, itemID, "workflow:invoice-processing-456")
item, err := ib.Cancel(ctx, itemID, "user:sarah", "Duplicate item")
item, err := ib.Expire(ctx, itemID)
```

### Batch operations with Op builder

The `Op` builder collects multiple mutations and events on an item and flushes them in a single entity store write.

```go
item, err := ib.On(ctx, itemID, "user:compliance:fatima").
    Respond("approve", "False positive — name similarity only.").
    UpdatePayload(typeURL, resolvedPayload).
    Comment("Checked DOB and nationality against OFAC list. No match.").
    Tag("resolved:cleared").
    TransitionTo(inbox.StatusCompleted).
    Apply()
```

#### Custom domain events

Use `WithEvent` to emit custom proto events. `Event.Type` is derived from the proto message name.

```go
item, err := ib.On(ctx, itemID, "agent:kyc-bot").
    WithEvent(&kycpb.IDVCompleted{
        LivenessPassed:    true,
        FacialMatchPassed: true,
        Confidence:        0.98,
    }).
    WithEvent(&kycpb.AddressVerified{
        Validated: true,
        Method:    "utility_bill",
    }).
    Comment("All automated checks passed.").
    Apply()
```

#### Op builder methods

| Method | Purpose |
|---|---|
| `Respond(action, comment)` | Record a response |
| `WithEvent(proto.Message)` | Emit a typed domain event |
| `UpdatePayload(typeURL, data)` | Replace the item payload |
| `Comment(body)` | Add a comment |
| `CommentWith(body, opts)` | Add a comment with visibility/refs |
| `Tag(tags...)` | Add tags |
| `Untag(tags...)` | Remove tags |
| `TransitionTo(status)` | Change status |
| `Apply()` | Flush all mutations in one write |

### Comments, escalation, reassignment

```go
ib.Comment(ctx, itemID, "user:sarah", "Spoke with client.", nil)
ib.Comment(ctx, itemID, "user:sarah", "Internal note.",
    &inbox.CommentOpts{Visibility: []string{"team:compliance"}})

ib.Escalate(ctx, itemID, "user:sarah", "ops", "compliance", "Needs EDD")
ib.Reassign(ctx, itemID, "user:manager", "user:sarah", "user:ahmed", "PEP specialist")
```

### Tags

```go
ib.Tag(ctx, itemID, "user:sarah", "priority:urgent", "escalated")
ib.Untag(ctx, itemID, "user:sarah", "priority:normal")

inbox.TagValue(item, "team:")            // "compliance"
inbox.TagsWithPrefix(item, "ref:")       // ["ref:invoice:INV-1234"]
inbox.HasTag(item, "priority:urgent")    // true
```

### Querying

```go
ib.ListByTags(ctx, []string{"status:open", "team:compliance"}, inbox.ListOpts{PageSize: 20})
ib.Search(ctx, "PEP screening Ahmed", inbox.ListOpts{})
ib.SemanticSearch(ctx, embeddingVector, 10)
ib.Stale(ctx, []string{"status:open", "priority:urgent"}, 2*time.Hour, inbox.ListOpts{})
```

### Reading events

```go
for _, evt := range item.Events {
    fmt.Printf("[%s] %s\n", evt.Type, evt.Actor)

    switch evt.Type {
    case "inbox.v1.CommentAppended":
        var c inboxv1.CommentAppended
        json.Unmarshal(evt.Data, &c)

    case "kyc.v1.IDVCompleted":
        var idv kycpb.IDVCompleted
        json.Unmarshal(evt.Data, &idv)
    }
}
```

## How it's stored

```
entity_type: "inbox.v1.Item"
tags:        ["type:review", "team:compliance", "status:open", "priority:high"]
data (JSONB): {
  "title": "PEP screening review — Ahmed K.",
  "status": "open",
  "payload_type": "eligibility.v1.EligibilityReviewPayload",
  "payload": { "subscription_id": "SUB-2026-0042", ... },
  "events": [
    {
      "at": "2026-03-18T10:00:00Z",
      "actor": "workflow:onboarding-456",
      "type": "inbox.v1.ItemCreated",
      "data": { "payload_type": "eligibility.v1.EligibilityReviewPayload" }
    },
    {
      "at": "2026-03-18T14:30:00Z",
      "actor": "agent:kyc-bot",
      "type": "kyc.v1.IDVCompleted",
      "data": { "liveness_passed": true, "confidence": 0.98 }
    }
  ]
}
```

## Architecture decisions

See [doc/adr/001-inbox-on-entity-store.md](doc/adr/001-inbox-on-entity-store.md) for the full ADR covering:
- Why entity store over a dedicated table
- Tags over relations
- Typed proto payloads for analytics
- Workflow callback integration
- Why reminders/escalations are external policies
