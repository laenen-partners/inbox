# CLAUDE.md

## Project overview

Task-driven inbox system backed by the entity store. Inbox items are single actionable units of work resolved by humans or AI agents. The inbox is a service — external modules interact via Connect-RPC client, not Go imports.

## Tools

Tools are managed via [mise](https://mise.jdx.dev/). Run `mise install` to install all required tools (Go, buf, task, gofumpt, golangci-lint, etc.). See `mise.toml` for versions.

## Build & test

All commands run via [Task](https://taskfile.dev/). See `Taskfile.yaml` for all available tasks.

```bash
task generate          # proto + templ generation
task build             # go build ./...
task format            # gofumpt + templ fmt + buf format
task lint              # golangci-lint + buf lint
task test:ci           # tests with JUnit output (requires Docker for testcontainers)
task ci                # full CI pipeline: setup → generate → format → lint → build → test → vuln
```

## Key conventions

### Three states only

Items have three states: `open`, `claimed`, `closed`. The inbox owns claim/release. External modules close items when done via `CloseItem` RPC.

### Events are proto messages

Every event on an inbox item is a proto message. The `Event.DataType` field is the fully qualified proto message name, derived automatically via `proto.MessageName(msg)`. Never set `DataType` manually — it comes from the proto.

### No hand-written type constants

Do not create string constants for event types, action names, or type URLs. These are all derived from the proto message at runtime. If you need to compare, use the proto message name directly: `"inbox.v1.ItemClaimed"`.

### Proto-first

All structured data types (events, payloads) must be proto messages. Define new event types in `proto/inbox/v1/events.proto`, run `buf generate`, use the generated types. Do not create plain Go structs for event data.

### Entity store types, not custom types

Use the generated `inboxv1.*` types directly (e.g. `&inboxv1.ItemClaimed{}`). Do not create type aliases or re-exports.

### Tags, not fields

Routing, classification, and linking use entity store tags (`key:value` strings). Do not add proto fields for things like priority, team, assignee, or workflow links.

### State is mutable, events are audit

The item's current state (`status`, `payload`, `tags`) is the source of truth. Events are an append-only audit log. Never replay events to reconstruct state.

### Idempotency via anchor

Set `Meta.IdempotencyKey` to prevent duplicate item creation. The key becomes an entity store anchor. Leave empty for no dedup (default).

### Service boundary

External modules interact with the inbox via Connect-RPC client. The inbox UI renders templates; external modules provide content via `ContentProvider` interface. Action handlers in the inbox UI re-render the drawer and queue row directly — do NOT use `bus.NotifyUpdated` from action handlers as the stream-triggered queue morph closes the drawer.

### Reactive updates via stream

The queue table uses `stream.Watch` with `stream.Any` for reactive updates from external changes (other users, modules calling RPCs). Action handlers patch the drawer and queue row directly in the same SSE response instead.

## File layout

```
inbox.go          — Inbox type, constructor, EntityType
item.go           — Item, Meta, ListOpts, CommentOpts, Pack/Unpack helpers
status.go         — Status constants (open, claimed, closed), IsTerminal
create.go         — Create, internal marshaling/tokenization
get.go            — Get, ListByTags, Search, SemanticSearch, Stale
lifecycle.go      — Claim, Release, Close
events.go         — AddEvent, Comment, Reassign
tags.go           — Tag, Untag, TagValue, HasTag, TagsWithPrefix
errors.go         — Error constants
proto/inbox/v1/   — Proto definitions (item.proto, events.proto, service.proto)
gen/inbox/v1/     — Generated Go code (do not edit)
service/          — Connect-RPC service handler
ui/               — Inbox web UI (queue, detail, search, mywork)
cmd/inboxui/      — Showcase demo app (schema, token, seed data)
```

## Code generation

```bash
task generate          # runs both proto and templ generation
task generate:proto    # buf generate
task generate:templ    # go tool templ generate
```

Proto uses two plugins:
- `buf.build/protocolbuffers/go` — standard Go protobuf
- `protoc-gen-entitystore` — generates `ItemMatchConfig()` and `ItemExtractionSchema()` from entitystore annotations

## Service RPCs

```
CreateItem, GetItem, ListItems, SearchItems,
ClaimItem, ReleaseItem, ReassignItem,
CommentOnItem, AddEvent, CloseItem,
TagItem, UntagItem
```

## Events

```
ItemCreated, ItemClaimed, ItemReleased, ItemClosed,
CommentAppended, ItemReassigned, TagsChanged
```

External modules define their own domain-specific event protos and record them via `AddEvent` RPC.
