# ADR-001: Inbox Items on the Entity Store

- **Status:** Proposed
- **Date:** 2026-03-18
- **Author:** Pascal Laenen

## Context

We need a task-driven inbox system where **inbox items** represent units of work
to be resolved by a human or an AI agent. Items are created by external systems
(workflows, classifiers, extractors, scheduled jobs) and must be flexible enough
to carry arbitrary metadata, be tagged freely, and participate in async
processes.

An earlier prototype (`.tmp/inbox/`) was tightly coupled to project-m's internal
Postgres schema and DBOS workflow runtime. While functional, it cannot be reused
across services and locks the inbox concept into a single application boundary.

Meanwhile, the **entity store** (`github.com/laenen-partners/entitystore`)
already provides exactly the storage primitives we need: typed JSONB entities,
first-class tags (GIN-indexed), relations between entities, vector embeddings
for semantic search, and provenance tracking. It is used successfully by the
product catalog and is designed for multi-tenant, cross-service use.

### AI-Native Design Considerations

Modern human-in-the-loop systems (Linear, GitHub Issues, Anthropic tool-use
loops, LangGraph interrupt nodes) share a pattern: a **work item** that
accumulates context over time and can be acted upon by either a human or an AI
agent identically. Key observations:

1. **Items, not threads.** The atomic unit is a task/decision, not a
   conversation. Context accumulates as an append-only event log on the item,
   but the item itself remains a single addressable entity with a clear status.
2. **Semantic discoverability.** AI agents need to *find* relevant items — not
   just be handed one. Embeddings on title + description let agents query
   "items similar to X" without predefined routing rules.
3. **Tags as routing.** Rather than hardcoded queues or assignment rules, tags
   act as a flexible routing and filtering mechanism. An agent or human
   subscribes to tags, not to a specific queue.
4. **Typed proto payloads.** Each item carries a `google.protobuf.Any`
   payload — the inbox stores and delivers it without interpretation, but
   the fully qualified proto message name gives consumers type-safe
   deserialization, schema versioning, and analytics for free.
5. **Tags replace hierarchy.** Instead of parent/child item tables or
   formal entity relations, tags like `workflow:X`, `blocks:item:Y`,
   `ref:invoice:Z` model all linking. This avoids requiring the other
   side to exist as an entity and keeps the model flat and simple.

## Decision

**Inbox items are entities in the entity store.** Each item is persisted as an
entity of type `inbox.item` with the entity store as the sole persistence
backend. We do not maintain a separate `inbox_items` table.

### Entity Model

The proto definition lives at `proto/inbox/v1/item.proto`. The design
principle is **proto = content + state, tags = everything else**.

**Proto fields (JSONB data)** — the minimum to display and mutate an item:

| Field         | Type              | Purpose                                         |
|---------------|-------------------|-------------------------------------------------|
| `title`       | string            | Human-readable summary (embedded + tokenized)   |
| `description` | string            | Context / instructions (embedded + tokenized)   |
| `status`      | string            | Lifecycle: open, claimed, completed, expired, cancelled |
| `deadline`    | Timestamp         | Optional expiry time                            |
| `payload`     | Any               | Typed proto payload via `google.protobuf.Any` — inbox stores it, doesn't interpret it |
| `events`      | repeated Event    | Append-only activity log                        |

**Tags** — all routing, classification, and linking:

| Tag pattern              | Example                        | Replaces              |
|--------------------------|--------------------------------|-----------------------|
| `type:<subtype>`         | `type:approval`                | data.type field       |
| `priority:<level>`       | `priority:urgent`              | data.priority field   |
| `team:<name>`            | `team:finance`                 | data.team field       |
| `assignee:<actor>`       | `assignee:pascal`              | data.principal field  |
| `source:<urn>`           | `source:workflow:inv-123`      | triggered_by relation |
| `workflow:<id>`          | `workflow:invoice-approval-456`| part_of_workflow relation |
| `ref:<type>:<id>`        | `ref:invoice:789`              | references relation   |
| `blocks:<item-id>`       | `blocks:item:abc`              | blocks relation       |
| `status:<state>`         | `status:open`                  | (mirrored from field) |
| `deadline:<RFC3339>`     | `deadline:2026-03-20T00:00:00Z`| (mirrored from field) |

This means adding a new priority level, team, item type, or link kind
requires zero proto changes and zero migrations — just a new tag value.

**Why tags over relations (for now):**
- Relations require both sides to exist as entities in the store.
- Tags are GIN-indexed and queryable today with no foreign key constraints.
- Tags work across system boundaries (a workflow ID doesn't need to be an entity).
- Relations can be introduced later if true graph traversal is needed.

**Anchors:** None by default. Items are unique. For idempotency, callers
can set a source_urn anchor to prevent duplicate creation.

**Tokens:** title, description — for fuzzy text search.

**Embedding:** Vector on title + description — for semantic similarity.

### Typed Payloads via `google.protobuf.Any`

The inbox does not interpret or own payload schemas. Both `Item.payload`
and `Event.data` use `google.protobuf.Any`, which carries:

- **`type_url`** — the fully qualified proto message name
  (e.g. `type.googleapis.com/forms.v1.ActionForm`)
- **`value`** — the serialized proto bytes

This gives consumers type-safe deserialization, schema versioning, and
a natural discriminator — all from the proto ecosystem. No custom
`payload_type` string, no JSON-schema-in-JSON. Payload and event data
proto definitions are owned by the packages that create and render
items, not by the inbox.

Example payload types:

| Proto message                    | Use case                         |
|----------------------------------|----------------------------------|
| `forms.v1.ActionForm`            | Single-page form (approve/reject)|
| `forms.v1.Journey`               | Multi-step flow (onboarding)     |
| `prodcat.v1.EligibilityReview`   | Eligibility conflict review      |
| `notifications.v1.Alert`         | Informational, no response       |

The inbox is a transport and storage layer — how payloads are defined,
rendered, or interpreted is not its concern.

**Observability and analytics.** Because every payload and event carries
a fully qualified proto message name, analytics systems get structured
access for free:

- **Discover** what item types exist: `SELECT DISTINCT type_url FROM ...`
- **Deserialize** any payload with the proto registry — full field-level
  access, no custom parsers or `jq` spelunking.
- **Aggregate** across types: counts by payload type, response times by
  event type, SLA breaches by team tag.
- **Schema-validate** payloads at ingestion or audit time against their
  proto definition.
- **Evolve** payload and event schemas independently with proto's
  backwards-compatible field additions.

#### Example: defining a payload proto

Payload protos are owned by the domain that creates the item. For example,
the prodcat package defines a payload for eligibility reviews:

```protobuf
// prodcat/proto/eligibility/v1/inbox_payloads.proto
syntax = "proto3";
package eligibility.v1;

// EligibilityReviewPayload is the inbox item payload when a subscription
// requirement needs manual review (e.g. PEP screening, source of funds).
message EligibilityReviewPayload {
  string subscription_id = 1;
  string product_id = 2;
  string product_name = 3;
  string requirement_name = 4;       // e.g. "pep_screening_clear"
  string category = 5;               // e.g. "kyc"
  string failure_mode = 6;           // "manual_review", "input_required"
  string resolution_hint = 7;        // human-readable guidance
  string customer_id = 8;
  string party_id = 9;
}
```

The workflow packs this into the inbox item:

```go
payload, _ := anypb.New(&eligibilityv1.EligibilityReviewPayload{
    SubscriptionId:  "SUB-2026-0042",
    ProductId:       "casa-aed",
    ProductName:     "Current Account — AED",
    RequirementName: "pep_screening_clear",
    Category:        "kyc",
    FailureMode:     "manual_review",
    ResolutionHint:  "Customer flagged as PEP. Review screening report and approve or reject.",
    CustomerId:      "CUST-1234",
    PartyId:         "PARTY-5678",
})

inbox.Create(ctx, es, inbox.Meta{
    Title:       "PEP screening review — Ahmed K.",
    Description: "Customer flagged as PEP during onboarding for Current Account — AED.",
    Payload:     payload,
    Tags:        []string{"type:review", "team:compliance", "priority:high",
                          "ref:subscription:SUB-2026-0042", "workflow:onboarding-456"},
})
```

Any consumer — compliance UI, analytics pipeline, AI agent — reads the
`type_url`, finds `eligibility.v1.EligibilityReviewPayload` in the proto
registry, and has typed access to every field.

#### Example: defining an event data proto

Event data protos follow the same pattern. The inbox defines a few
common ones; domains can add their own:

```protobuf
// inbox/proto/inbox/v1/events.proto
syntax = "proto3";
package inbox.v1;

import "google/protobuf/any.proto";

// ResponseEvent is recorded when someone responds to an item.
message ResponseEvent {
  string action = 1;            // e.g. "approve", "reject", "submit"
  string comment = 2;
  string on_behalf_of = 3;      // set when an RM acts for a client
  string override_reason = 4;   // justification for override
  google.protobuf.Any payload = 5;  // response-specific typed data
}

// TagChangeEvent is recorded when tags are added or removed.
message TagChangeEvent {
  repeated string added = 1;
  repeated string removed = 2;
}

// EscalationEvent is recorded when an item is escalated.
message EscalationEvent {
  string from_team = 1;
  string to_team = 2;
  string reason = 3;
}
```

An analytics query like "average time from creation to first response
for compliance reviews this month" becomes straightforward: filter by
`type_url` on both the item payload and the response event, join on
timestamps.

#### How it's stored in the entity store

The inbox item proto is serialized to JSON and stored in the entity
store's `data` (JSONB) column. `google.protobuf.Any` fields are
serialized using protobuf's canonical JSON mapping (`@type` + fields):

```json
{
  "entity_type": "inbox.item",
  "tags": ["type:review", "team:compliance", "priority:high",
           "status:open", "ref:subscription:SUB-2026-0042"],
  "data": {
    "title": "PEP screening review — Ahmed K.",
    "description": "Customer flagged as PEP during onboarding.",
    "status": "open",
    "deadline": "2026-03-20T00:00:00Z",
    "payload": {
      "@type": "type.googleapis.com/eligibility.v1.EligibilityReviewPayload",
      "subscriptionId": "SUB-2026-0042",
      "productId": "casa-aed",
      "productName": "Current Account — AED",
      "requirementName": "pep_screening_clear",
      "category": "kyc",
      "failureMode": "manual_review",
      "customerId": "CUST-1234",
      "partyId": "PARTY-5678"
    },
    "events": [
      {
        "at": "2026-03-18T10:00:00Z",
        "actor": "workflow:onboarding-456",
        "action": "created"
      },
      {
        "at": "2026-03-18T14:30:00Z",
        "actor": "user:rm:sarah",
        "action": "responded",
        "data": {
          "@type": "type.googleapis.com/inbox.v1.ResponseEvent",
          "action": "approve",
          "comment": "Reviewed screening report, client is a former minister — low risk.",
          "onBehalfOf": "customer:CUST-1234",
          "overrideReason": "PEP status is historical, no current political exposure"
        }
      }
    ]
  }
}
```

The `@type` field in the JSON is what makes this queryable. Analytics
can filter on `data->'payload'->>'@type'` to find all items of a
specific payload type, and deserialize with the proto registry for
full field access. Events are similarly queryable by their `@type`.

### Event Log (Lightweight Thread)

Rather than a separate comments/activity table, each item carries an
append-only `events` array in its JSONB data. This keeps the item
self-contained and queryable without joins.

```json
{
  "events": [
    {"at": "2026-03-18T10:00:00Z", "by": "workflow:inv-123", "action": "created"},
    {"at": "2026-03-18T10:05:00Z", "by": "agent:triage-bot", "action": "tagged", "detail": "priority:urgent"},
    {"at": "2026-03-18T10:30:00Z", "by": "user:pascal", "action": "claimed"},
    {"at": "2026-03-18T10:45:00Z", "by": "user:pascal", "action": "commented", "detail": "Checked with supplier, amount is correct."},
    {"at": "2026-03-18T11:00:00Z", "by": "user:pascal", "action": "responded", "detail": "approve"}
  ]
}
```

Events are written via **merge** (JSONB `||` with array append), not full
replacement, to avoid lost-update races.

### API Surface (Go package)

The `inbox` package wraps the entity store and exposes a focused API:

```go
// Core lifecycle
Create(ctx, es, meta Meta) (Item, error)
Get(ctx, es, itemID string) (Item, error)
Claim(ctx, es, itemID string, actor string) error
Respond(ctx, es, itemID string, resp Response) error
Cancel(ctx, es, itemID string, actor string) error
Expire(ctx, es, itemID string) error

// Event log
AddEvent(ctx, es, itemID string, evt Event) error

// Queries (delegate to entity store)
ListByTags(ctx, es, tags []string, opts ListOpts) ([]Item, error)
Search(ctx, es, query string, opts ListOpts) ([]Item, error)         // token search
SemanticSearch(ctx, es, vec []float32, topK int) ([]Item, error)     // embedding search
Stale(ctx, es, tags []string, age time.Duration, opts ListOpts) ([]Item, error) // items with no recent activity

// Tag management (thin wrapper)
Tag(ctx, es, itemID string, tags ...string) error
Untag(ctx, es, itemID string, tag string) error
```

The entity store instance (`es`) is passed in — no global singleton. This
makes the package testable, composable, and free of hidden dependencies.

### Workflow Integration

The inbox is **not** coupled to a workflow runtime, but it provides a
direct callback mechanism so workflows don't need to poll.

**How it works:**

1. Workflow creates an inbox item. The item's payload includes a
   **callback** — a webhook URL or a workflow-native address (e.g.
   DBOS workflow ID + topic). This is stored as a tag:
   `callback:https://...` or `callback:dbos:<workflow-id>:<topic>`.
2. The item renders as a mini UI (form fields, actions — all driven
   by the payload schema).
3. When a user, RM, or AI agent responds, the inbox fires the callback
   with the response. The workflow receives it directly.
4. The workflow owns the lifecycle from here — it updates the item
   (merge new data, change status) or deletes it. The inbox doesn't
   assume what "done" looks like.

**The inbox's only job at response time** is:
- Record the response as an event on the item.
- Fire the callback (best-effort, with the response payload).

It does **not** change the item's status to "completed" — the workflow
does that, because only the workflow knows whether the response was
sufficient. An RM override might need a second approval; a form
submission might need validation against prodcat rules before the
item can close.

**Callback dispatch** is a pluggable interface:

```go
// Dispatcher delivers a response to the item's creator.
type Dispatcher interface {
    Dispatch(ctx context.Context, callback string, itemID string, resp Response) error
}
```

Concrete implementations (webhook, DBOS send, NATS publish, etc.) are
injected at configuration time, not baked into the package.

### Reminders, Locks, and Extensions

Because items are just entities with tags:

- **Reminders:** A background job queries items tagged `status:open` with a
  deadline in the past and calls `Expire()` or sends a notification.
- **Locks/Claims:** Claiming an item is a status transition (`open → claimed`)
  with optimistic concurrency via the entity store's `UpdatedAt` field.
- **SLA tracking:** A background job queries items by `created_at` and priority
  tags to flag breaches.
- **AI auto-resolve:** An agent queries open items by embedding similarity,
  inspects the payload, and calls `Respond()` if confident.

These are all built *on top of* the inbox API, not baked into it.

### Why Reminders and Escalations Are Not in the Inbox Package

Reminders ("notify if unclaimed after 1h") and escalations ("re-tag as
urgent after 2h") are **policies**, not inbox primitives. They vary per
team, per item type, and per deployment. Baking them into the inbox proto
or API would mean:

- Config fields that most items don't use.
- A scheduler/timer engine inside a package that should be pure CRUD.
- Coupling to notification channels (Slack, email, webhooks).

Instead, the inbox already exposes everything a policy engine needs:

- **`deadline`** — first-class expiry.
- **`events[]`** — last activity timestamp.
- **Tags** — `priority:urgent`, `team:finance`, `status:open` for filtering.
- **`created_at` / `updated_at`** — from the entity store.

To make policy logic easy to build, the inbox API includes a query helper:

```go
// Stale returns items matching tags where the last event is older than the given age.
Stale(ctx, es, tags []string, age time.Duration, opts ListOpts) ([]Item, error)
```

A separate policy/rules layer (cron job, workflow, or rules engine) queries
the inbox via `Stale` and `ListByTags`, checks timestamps, and takes
actions — tag changes, notifications, escalations. That layer owns its own
config and lives outside this package.

## Consequences

### Positive

- **Reusable.** Any service with access to the entity store can create and
  query inbox items. No coupling to DBOS, project-m, or any specific app.
- **Flexible.** Tags + JSONB payload mean new item types require zero schema
  migrations.
- **AI-native.** Embeddings and token search let AI agents discover and
  triage items without explicit routing.
- **Auditable.** Provenance tracking is built into the entity store. Every
  write records who/what/when.
- **Linkable.** Tags like `ref:invoice:789` and `workflow:inv-456` connect
  items to anything without requiring formal entity relations or foreign keys.

### Negative

- **Event log in JSONB.** Appending to an array in JSONB is not as efficient
  as an append-only table. For items with very long activity histories (100+
  events), we may need to spill to a separate entity type. Acceptable for
  now; most items will have < 20 events.
- **No built-in pub/sub.** The entity store is pull-based. Real-time
  notifications require a side-channel (webhooks, SSE, or a message broker).
  This is intentionally out of scope for the inbox package.
- **Optimistic concurrency only.** Two actors claiming the same item
  simultaneously could race. We mitigate with a compare-and-swap on status +
  `updated_at`, which is sufficient for the expected concurrency levels.

## Alternatives Considered

### 1. Dedicated inbox_items table (current prototype)

Simpler initial setup but creates a parallel persistence layer that duplicates
tag, relation, and search capabilities the entity store already has.
Rejected because it fragments the data model.

### 2. Items as threads (conversation-first model)

Making each item a thread with messages treats everything as a conversation.
This adds unnecessary complexity for items that are simple approve/reject
decisions. The event log within the item gives "thread-like" context without
the overhead of a full messaging model. Rejected as over-engineering for the
common case.

### 3. Separate event/activity table

Storing events in their own table (or as separate entities) instead of inline
in the item's JSONB. More normalized but adds joins for every item read and
breaks the "item is self-contained" property. Can be adopted later if event
volumes justify it. Deferred.
