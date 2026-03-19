# Changelog

## v0.1.0 (2026-03-19)

Initial release of the inbox module.

### Features

- **Entity store backend** — inbox items are entities in the entity store (`inbox.v1.Item`), no separate table
- **Tag-based routing** — all filtering, classification, and linking via GIN-indexed tags (`type:`, `team:`, `priority:`, `assignee:`, `workflow:`, `ref:`, `status:`)
- **Proto-native** — all data types (items, events, payloads) are proto messages with full type safety and schema versioning
- **Typed events** — every operation produces a typed event (`inbox.v1.ItemClaimed`, `inbox.v1.CommentAppended`, etc.) with `DataType` derived from `proto.MessageName`
- **Op builder** — batch multiple mutations and events in a single entity store write via `ib.On(ctx, id, actor).Respond(...).WithEvent(...).Comment(...).TransitionTo(...).Apply()`
- **Custom domain events** — `WithEvent(proto.Message)` emits any proto as a typed event
- **Lifecycle management** — `Create`, `Claim`, `Release`, `Respond`, `Complete`, `Cancel`, `Expire`
- **Comments** — with optional visibility restrictions and entity references
- **Escalation and reassignment** — with automatic tag updates
- **Idempotency** — optional `IdempotencyKey` field as entity store anchor for dedup
- **Query helpers** — `ListByTags`, `Search` (fuzzy text), `SemanticSearch` (embeddings), `Stale` (no recent activity)
- **Callback dispatcher** — pluggable `Dispatcher` interface for workflow integration
- **Payload-agnostic** — inbox stores and delivers proto payloads without interpretation
- **State is mutable, events are audit** — current state in the entity, events as append-only log
- **buf codegen** — `protoc-gen-entitystore` generates `ItemMatchConfig()` and `ItemExtractionSchema()` from proto annotations
- **9 e2e tests** — KYC/onboarding scenarios with testcontainers/postgres

### Dependencies

- `github.com/laenen-partners/entitystore` v0.10.0
- `google.golang.org/protobuf` v1.36.11
