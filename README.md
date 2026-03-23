# inbox

A task-driven inbox for GenAI-first applications, backed by the [entity store](https://github.com/laenen-partners/entitystore). Inbox items are single actionable units of work — approvals, reviews, data collection, compliance checks, or any async decision point in a workflow.

The inbox is a **service**. External modules interact via Connect-RPC client, not Go imports. The inbox owns claim/release semantics; external modules drive domain logic and close items when done.

## Architecture

```
External Module                    Inbox Service                 Entity Store
     |                                  |                             |
     |  CreateItem RPC                  |                             |
     |--------------------------------->|  Create (status: open)      |
     |                                  |----------------------------->|
     |                                  |                             |
     |  User claims via UI              |                             |
     |                                  |  Claim (status: claimed)    |
     |                                  |----------------------------->|
     |                                  |                             |
     |  User interacts with             |                             |
     |  external module template        |                             |
     |                                  |                             |
     |  AddEvent RPC                    |                             |
     |--------------------------------->|  Record domain event        |
     |                                  |----------------------------->|
     |                                  |                             |
     |  CloseItem RPC                   |                             |
     |--------------------------------->|  Close (status: closed)     |
     |                                  |----------------------------->|
```

### Item lifecycle

```
            Create
              |
              v
+--------+  Claim   +---------+
|  open  | -------> | claimed |
+--------+          +---------+
    ^                    |
    |     Release        |
    +--------------------+
    |                    |
    v                    v
         +---------+
         | closed  |   (set by external module via CloseItem RPC)
         +---------+
```

Three states: `open`, `claimed`, `closed`. The inbox owns claim/release. External modules close items when done.

## Service RPCs

| RPC | Purpose |
|-----|---------|
| `CreateItem` | Create a new inbox item |
| `GetItem` | Fetch a single item |
| `ListItems` | List items by tags |
| `SearchItems` | Fuzzy text search |
| `ClaimItem` | Assign item to current actor |
| `ReleaseItem` | Return item to open pool |
| `ReassignItem` | Move between actors |
| `CommentOnItem` | Add activity note |
| `AddEvent` | Record domain-specific event |
| `CloseItem` | Close item (terminal) |
| `TagItem` | Add tags |
| `UntagItem` | Remove tag |

## Events

| Event | Emitted by |
|-------|------------|
| `inbox.v1.ItemCreated` | `Create` |
| `inbox.v1.ItemClaimed` | `Claim` |
| `inbox.v1.ItemReleased` | `Release` |
| `inbox.v1.ItemClosed` | `Close` |
| `inbox.v1.CommentAppended` | `Comment` |
| `inbox.v1.ItemReassigned` | `Reassign` |
| `inbox.v1.TagsChanged` | `Tag` / `Untag` |

External modules define their own domain-specific event protos and record them via `AddEvent` RPC.

## Concepts

### Items are entities

Every inbox item is an entity in the entity store (type `inbox.v1.Item`). Items get tags, embeddings, token search, and all entity store features for free.

### Tags as routing

Items are routed and filtered entirely via tags — free-form `key:value` strings stored in the entity store's GIN-indexed tags column.

| Tag | Purpose |
|-----|---------|
| `type:approval` | Item kind |
| `priority:urgent` | Urgency level |
| `team:compliance` | Owning team |
| `assignee:user:sarah` | Assigned human or agent |
| `workflow:onboarding-456` | Parent workflow |
| `ref:invoice:789` | Link to a related entity |
| `status:open` | Mirrored from the status field |

### Typed payloads

Items carry a `payload_type` (fully qualified proto message name) and a `payload` (serialized proto). The inbox stores and delivers payloads without interpretation — payload schemas are owned by the domain that creates the item.

### Typed events

Every operation produces a typed event. The `Event.DataType` field is the fully qualified proto message name, derived automatically from the proto message.

### State is not derived from events

The inbox is **not** event-sourced. The item's current state is the source of truth. Events are an append-only audit log.

## Development

Tools are managed via [mise](https://mise.jdx.dev/). Commands run via [Task](https://taskfile.dev/).

```bash
mise install               # install all tools (Go, buf, task, gofumpt, etc.)
task generate              # proto + templ code generation
task build                 # go build ./...
task format                # gofumpt + templ fmt + buf format
task lint                  # golangci-lint + buf lint
task test:ci               # run tests (requires Docker for testcontainers)
task ci                    # full CI pipeline
```

## Usage

### Setup

```go
es, _ := entitystore.New(entitystore.WithPgStore(pool))
ib := inbox.New(es)
```

### Creating items (via RPC)

```go
client.CreateItem(ctx, connect.NewRequest(&inboxv1.CreateItemRequest{
    Identity: identity,
    Title:    "PEP screening review",
    Description: "Customer flagged as PEP during onboarding.",
    Tags:     []string{"type:review", "team:compliance", "priority:high"},
    PayloadType: "eligibility.v1.EligibilityReviewPayload",
    Payload:  payloadAny,
}))
```

### Lifecycle (via RPC)

```go
client.ClaimItem(ctx, connect.NewRequest(&inboxv1.ClaimItemRequest{
    Identity: identity, Id: itemID,
}))

client.ReleaseItem(ctx, connect.NewRequest(&inboxv1.ReleaseItemRequest{
    Identity: identity, Id: itemID,
}))

client.CloseItem(ctx, connect.NewRequest(&inboxv1.CloseItemRequest{
    Identity: identity, Id: itemID, Reason: "approved",
}))
```

### Adding events (via RPC)

```go
client.AddEvent(ctx, connect.NewRequest(&inboxv1.AddEventRequest{
    Identity: identity,
    Id:       itemID,
    Detail:   "Automated check passed",
    DataType: "kyc.v1.IDVCompleted",
    Data:     eventAny,
}))
```

### Comments (via RPC)

```go
client.CommentOnItem(ctx, connect.NewRequest(&inboxv1.CommentOnItemRequest{
    Identity: identity, Id: itemID,
    Body: "Checked screening report, no match.",
}))
```

### Querying (via Go API)

```go
ib.ListByTags(ctx, []string{"status:open", "team:compliance"}, inbox.ListOpts{PageSize: 20})
ib.Search(ctx, "PEP screening", inbox.ListOpts{})
ib.SemanticSearch(ctx, embeddingVector, 10)
ib.Stale(ctx, []string{"status:open", "priority:urgent"}, 2*time.Hour, inbox.ListOpts{})
```

## UI

The inbox UI is a mountable `chi.Router` that provides queue, detail drawer, search, and my-work views. External modules implement `ContentProvider` to render custom payload content inside the detail drawer.

```go
r.Mount("/inbox", inboxui.Handler(client,
    inboxui.WithBasePath("/inbox"),
    inboxui.WithBus(bus),
    inboxui.WithLayout(myLayout),
    inboxui.WithContentProvider("my.v1.Payload", myProvider{}),
    inboxui.WithIdentity(identityFn),
    inboxui.WithFilter(inboxui.FilterConfig{
        Label: "Team", TagPrefix: "team:",
        Options: []string{"compliance", "ops", "finance"},
    }),
))
```

### Reactive updates

The queue table uses `stream.Watch` with `stream.Any` for reactive updates when items change via external RPCs. Action handlers (claim, release, close, comment) update the drawer and queue row directly in the same SSE response.

## Package structure

| Package | Purpose |
|---------|---------|
| `inbox/` | Core: item lifecycle, events, tags, queries |
| `service/` | Connect-RPC service handler |
| `ui/` | Web UI: queue, detail, search, mywork |
| `cmd/inboxui/` | Showcase demo app (not part of the public API) |
