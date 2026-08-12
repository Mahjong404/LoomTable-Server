# Server Source Layout

LoomTable Server is a modular monolith. Packages are grouped by durable behavior, not by one route or one database table.

```text
cmd/
├── loomtable-server/     HTTP process composition root
├── loomtable-migrate/    explicit forward migration command
└── loomtable-admin/      local authentication administration command
internal/
├── auth/                 token generation, hashing, and authentication rules
├── catalog/              Workspace, Base, Table, Field, and View behavior
├── record/               Mutation, Query, Change, and Map behavior
├── domain/               shared domain values and stable errors
├── cursor/               signed cursor envelopes and purpose isolation
├── httpapi/              HTTP adapter, decoding, and error mapping
├── storage/postgres/     PostgreSQL adapter and transaction implementation
├── config/               environment-to-runtime configuration
├── id/                   typed identifier generation and validation
└── status/               readiness dependency states
migrations/               ordered PostgreSQL schema migrations
scripts/operations/       backup, validation, restore, and smoke-test entrypoints
```

Directories that are not implemented yet are shown to reserve their intended location; do not add empty placeholder directories.

## Module seams

### Catalog module

`internal/catalog` owns metadata invariants for Workspace, Base, Table, Field, and View. It normalizes names, validates Field definitions and View configurations, applies revision/no-op rules, and requests atomic persistence through its Store interface. Field and View do not become one-method pass-through packages.

### Record module

`internal/record` owns canonical values, mutation commands, filters, sorting, pagination, Change cursors, and Map result semantics. Query, Change, and Map share Field-aware validation and snapshot rules with Mutation instead of reimplementing them in HTTP handlers.

### Adapters

`internal/httpapi` translates HTTP requests and responses only. It may enforce transport concerns such as media type, body size, strict JSON decoding, authentication, and status-code mapping; business invariants remain behind the catalog or record interface.

`internal/storage/postgres` is the production adapter for catalog, record, auth, and operational persistence. P0 supports only PostgreSQL, so the codebase does not expose a generic multi-database interface. Store interfaces exist because production PostgreSQL and focused test fakes are both real adapters at those seams.

### Composition roots

Commands under `cmd/` construct dependencies and define process lifecycle. They do not contain reusable business logic. The normal Server process never runs migrations implicitly; migration and authentication administration remain explicit commands.

## Dependency rules

- `domain`, `id`, and `cursor` do not depend on HTTP or PostgreSQL.
- `catalog`, `record`, and `auth` do not import `httpapi`.
- `httpapi` depends on module interfaces and domain results, not SQL details.
- `storage/postgres` implements module Store interfaces and owns transaction boundaries required for atomic behavior.
- Operational scripts use public commands and documented storage contracts; they do not become an alternate business interface.

Tests exercise observable behavior through each module interface. PostgreSQL integration tests verify the production adapter and transaction semantics; HTTP tests verify only transport and mapping behavior.
