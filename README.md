# inbox

A task-driven inbox system backed by the [entity store](https://github.com/laenen-partners/entitystore). Inbox items are units of work to be resolved by a human or AI agent — approvals, reviews, data collection, compliance checks, or any async decision point in a workflow.

## Concepts

### Items are entities

Every inbox item is an entity in the entity store (type `inbox.item`). This means items get tags, embeddings, token search, provenance tracking, and all other entity store features for free. There is no separate `inbox_items` table.

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

Every state change, comment, and action on an item produces a typed event. Each event has a `type` field set to the fully qualified proto message name (e.g. `inbox.v1.ItemClaimed`), derived automatically from the proto message — never set manually. Events are append-only and form a lightweight thread on each item.

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

When a workflow updates the payload (e.g. after re-evaluating eligibility rules), it calls `UpdatePayload()` and the item reflects the latest state immediately. Consumers always read current state, never reconstruct it.

Events are an **audit log**, not a projection source. They tell you what happened, not what the current state is.

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

// Create the entity store.
pool, _ := pgxpool.New(ctx, connString)
es, _ := entitystore.New(entitystore.WithPgStore(pool))

// Create the inbox.
ib := inbox.New(es)

// With a callback dispatcher for workflow integration:
ib := inbox.New(es, inbox.WithDispatcher(myWebhookDispatcher))
```

### Creating items

```go
// Simple approval item.
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
// Pack a proto payload.
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
    Tags: []string{
        "type:review",
        "team:compliance",
        "priority:high",
        "ref:subscription:SUB-2026-0042",
    },
})

// Or use the SetPayload convenience:
meta := inbox.Meta{Title: "...", Description: "..."}
inbox.SetPayload(&meta, &myProtoPayload)
item, err := ib.Create(ctx, meta)
```

### Item lifecycle

```go
// Claim an item.
item, err := ib.Claim(ctx, itemID, "user:sarah")

// Release back to the pool.
item, err := ib.Release(ctx, itemID, "user:sarah")

// Respond (does NOT complete the item — workflow decides).
item, err := ib.Respond(ctx, itemID, inbox.Response{
    Actor:   "user:sarah",
    Action:  "approve",
    Comment: "Verified against PO, amounts match.",
})

// Workflow completes the item after processing the response.
item, err := ib.Complete(ctx, itemID, "workflow:invoice-processing-456")

// Or cancel / expire.
item, err := ib.Cancel(ctx, itemID, "user:sarah", "Duplicate item")
item, err := ib.Expire(ctx, itemID)
```

### Batch operations with Op builder

The `Op` builder collects multiple mutations and events on an item and flushes them in a single entity store write. This is the recommended way to perform compound operations.

```go
// Compliance officer responds, updates payload, comments, and completes — one write.
item, err := ib.On(ctx, itemID, "user:compliance:fatima").
    Respond("approve", "False positive — name similarity only.").
    UpdatePayload(typeURL, resolvedPayload).
    Comment("Checked DOB and nationality against OFAC list. No match.").
    Tag("resolved:cleared").
    TransitionTo(inbox.StatusCompleted).
    Apply()
```

#### Custom domain events

Use `WithEvent` to emit custom proto events alongside standard operations. The `Event.Type` is derived automatically from the proto message name.

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
    Comment("All automated checks passed. Ready for final review.").
    Apply()
```

Every event gets:
- `type` set to the fully qualified proto message name (e.g. `kyc.v1.IDVCompleted`)
- `data` set to the proto message serialized as JSON
- `actor` inherited from the `On()` call
- `at` timestamped at `Apply()` time

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
| `TransitionTo(status)` | Change status (completed, cancelled, etc.) |
| `Apply()` | Flush all mutations in one write |

### Comments

```go
// Simple comment.
item, err := ib.Comment(ctx, itemID, "user:sarah", "Spoke with client, docs arriving tomorrow.", nil)

// Internal comment visible only to compliance.
item, err := ib.Comment(ctx, itemID, "user:sarah",
    "PEP status is historical — low risk.",
    &inbox.CommentOpts{Visibility: []string{"team:compliance"}},
)
```

### Escalation and reassignment

```go
// Escalate from ops to compliance.
item, err := ib.Escalate(ctx, itemID, "user:sarah", "ops", "compliance",
    "Sanctions screening flagged — needs compliance review")

// Reassign to a specific person.
item, err := ib.Reassign(ctx, itemID, "user:manager",
    "user:sarah", "user:ahmed", "Ahmed handles PEP reviews")
```

### Tags

```go
// Add tags.
err := ib.Tag(ctx, itemID, "user:sarah", "priority:urgent", "escalated")

// Remove a tag.
err := ib.Untag(ctx, itemID, "user:sarah", "priority:normal")

// Query helpers.
team := inbox.TagValue(item, "team:")           // "compliance"
refs := inbox.TagsWithPrefix(item, "ref:")       // ["ref:invoice:INV-1234"]
isUrgent := inbox.HasTag(item, "priority:urgent") // true
```

### Querying

```go
// List by tags.
items, err := ib.ListByTags(ctx, []string{"status:open", "team:compliance"}, inbox.ListOpts{PageSize: 20})

// Fuzzy text search.
items, err := ib.Search(ctx, "PEP screening Ahmed", inbox.ListOpts{})

// Semantic search (requires embeddings).
items, err := ib.SemanticSearch(ctx, embeddingVector, 10)

// Stale items (no activity for 2 hours).
items, err := ib.Stale(ctx, []string{"status:open", "priority:urgent"}, 2*time.Hour, inbox.ListOpts{})
```

### Unpacking payloads

```go
item, _ := ib.Get(ctx, itemID)

// Check the payload type.
fmt.Println(item.PayloadType) // "type.googleapis.com/eligibility.v1.EligibilityReviewPayload"

// Unpack into a concrete proto.
var review eligibilityv1.EligibilityReviewPayload
if err := inbox.UnpackPayload(item.Payload, &review); err != nil {
    // handle error
}
fmt.Println(review.RequirementName) // "pep_screening_clear"
```

### Reading events

```go
for _, evt := range item.Events {
    fmt.Printf("[%s] %s\n", evt.Type, evt.Actor)

    // Switch on the proto message name.
    switch evt.Type {
    case "inbox.v1.CommentAppended":
        var comment inboxv1.CommentAppended
        json.Unmarshal(evt.Data, &comment)
        fmt.Printf("  comment: %s (visibility: %v)\n", comment.Body, comment.Visibility)

    case "inbox.v1.ItemEscalated":
        var esc inboxv1.ItemEscalated
        json.Unmarshal(evt.Data, &esc)
        fmt.Printf("  escalated: %s → %s\n", esc.FromTeam, esc.ToTeam)

    case "kyc.v1.IDVCompleted":
        var idv kycpb.IDVCompleted
        json.Unmarshal(evt.Data, &idv)
        fmt.Printf("  IDV: liveness=%v confidence=%.2f\n", idv.LivenessPassed, idv.Confidence)
    }
}
```

## How it's stored

An inbox item maps to a single row in the entity store's `entities` table:

```
entity_type: "inbox.v1.Item"
tags:        ["type:review", "team:compliance", "status:open", "priority:high"]
data (JSONB): {
  "title": "PEP screening review — Ahmed K.",
  "description": "Customer flagged as PEP during onboarding.",
  "status": "open",
  "payload_type": "type.googleapis.com/eligibility.v1.EligibilityReviewPayload",
  "payload": {
    "@type": "type.googleapis.com/eligibility.v1.EligibilityReviewPayload",
    "subscriptionId": "SUB-2026-0042",
    ...
  },
  "events": [
    {
      "at": "2026-03-18T10:00:00Z",
      "actor": "workflow:onboarding-456",
      "type": "inbox.v1.ItemCreated",
      "data": {"payload_type": "type.googleapis.com/eligibility.v1.EligibilityReviewPayload"}
    },
    {
      "at": "2026-03-18T14:30:00Z",
      "actor": "agent:kyc-bot",
      "type": "kyc.v1.IDVCompleted",
      "data": {
        "liveness_passed": true,
        "facial_match_passed": true,
        "confidence": 0.98
      }
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
