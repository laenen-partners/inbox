# CLAUDE.md

## Project overview

Task-driven inbox system backed by the entity store. Inbox items are units of work resolved by humans or AI agents.

## Build & test

```bash
buf dep update && buf generate  # generate proto Go code
go build ./...                  # build
go test -v -count=1 -timeout 120s ./...  # test (requires Docker for testcontainers)
go vet ./...                    # lint
```

Or via task: `task generate`, `task build`, `task test`

## Key conventions

### Events are proto messages

Every event on an inbox item is a proto message. The `Event.Type` field is the fully qualified proto message name, derived automatically via `proto.MessageName(msg)`. Never set `Type` manually — it comes from the proto.

### No hand-written type constants

Do not create string constants for event types, action names, or type URLs. These are all derived from the proto message at runtime. If you need to compare, use the proto message name directly: `"inbox.v1.ItemClaimed"`.

### Proto-first

All structured data types (events, payloads) must be proto messages. Define new event types in `proto/inbox/v1/events.proto`, run `buf generate`, use the generated types. Do not create plain Go structs for event data.

### Entity store types, not custom types

Use the generated `inboxv1.*` types directly (e.g. `&inboxv1.ItemClaimed{}`). Do not create type aliases or re-exports.

### Event.Type is the single discriminator

The `Event` struct has: `At`, `Actor`, `Type`, `Detail`, `Data`. There is no `Action` field — `Type` (the proto message name) is what happened. `Detail` is optional human-readable context.

### Tags, not fields

Routing, classification, and linking use entity store tags (`key:value` strings). Do not add proto fields for things like priority, team, assignee, or workflow links.

### State is mutable, events are audit

The item's current state (`status`, `payload`, `tags`) is the source of truth. Events are an append-only audit log. Never replay events to reconstruct state.

### Idempotency via anchor

Set `Meta.IdempotencyKey` to prevent duplicate item creation. The key becomes an entity store anchor. Leave empty for no dedup (default).

## File layout

```
inbox.go          — Inbox type, constructor, EntityType
options.go        — WithDispatcher option
item.go           — Item, Event, Meta, Response, ListOpts, Pack/Unpack helpers
status.go         — Status constants, IsTerminal
create.go         — Create, internal marshaling/tokenization
get.go            — Get, ListByTags, Search, SemanticSearch, Stale
lifecycle.go      — Claim, Release, Respond, Complete, Cancel, Expire, UpdatePayload
events.go         — AddEvent, Comment, Escalate, Reassign, newTypedEvent
tags.go           — Tag, Untag, TagValue, HasTag, TagsWithPrefix
op.go             — Op builder (batch operations + WithEvent)
dispatcher.go     — Dispatcher interface
proto/inbox/v1/   — Proto definitions (item.proto, events.proto)
gen/inbox/v1/     — Generated Go code (do not edit)
```

## Proto code generation

```bash
buf dep update
buf generate
```

Uses two plugins:
- `buf.build/protocolbuffers/go` — standard Go protobuf
- `protoc-gen-entitystore` — generates `ItemMatchConfig()` and `ItemExtractionSchema()` from entitystore annotations
