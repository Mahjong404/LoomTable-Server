# G5 Table Audit — Server delivery

## Source and state

Implementation and full cloud QA source: **6f0abf1112652b4c1b0e7110b8cee0ede131815f**. Based on main **fc9ae843d7e2ef5d380ce31183e5d690420d5cb9** after G4 merge **36bedafcb8a3f9f117e1096b1fa63d7f0eed55b5**. OpenAPI **0.8.0**, capability **tableAudit**, migrations **010_table_audit / 011_audit_payload_privacy**. PR head may include delivery-only documentation; verify GitHub review/checks for its exact SHA before merge. No independent PASS is claimed for author self-review. Subagents: **0**.

Production read-only Meta checked before and after cloud QA: **v0.4.0+f3980b1b4126**, with choicePalette12/aggregateSearch and without atomicChoice/viewOrderCAS/querySelection/tableAudit. Production 007/008/009/010/011 were not executed. This delivery is not production deployment or Plugin Integration Ready.

The [contract and Writer coverage matrix](../contracts/g5-table-audit.md) define approved scope, exact identity, honest unknown legacy data and lifecycle. [OpenAPI](../../api/openapi.yaml) is authoritative transport. No full Table rollback or Bulk Mutation was implemented.

## Implementation and invariants

- New Audit domain/read service, signed filter-bound thirty-minute cursor and PostgreSQL Repeatable Read page/count/availability. Category=record is applied before limit and excludes order/metadata; count measures Events. New appends stay above the anchored high sequence. Retention generation changes return 410 rather than silently changing page counts.
- Transaction-local Operation IDs and additive X-LoomTable-Operation-Id response header. The original ID is stored separately in receipts and replayed before CAS; legacy JSON and request hashes are unchanged. Pre-G5 receipts have no invented ID. Receipt expiry/permanent G2/G3 guards remain unchanged.
- Row capture is in the business transaction; audit failure rolls back options, Record rows, Changes, receipt and guard. Multiple G2 effects share one Operation ID; exact legacy Change association uses object ID inside that transaction. No-op/replay have no extra Audit events.
- Record typed cells capture event-time Field/Option facts, missing/null/value distinction, tombstones and unknown metadata. Conversion supplies its planned after definition before old converter Record rewrites. G3 Default stays metadata; actual order changes are order. G4 membership hooks and existing G6 Query semantics remain intact.
- Ancestor projection locks descendant Tables in ascending ID order after the existing ancestor lock. Capture adds no Field/View/Record locks or shared global writer lock. Table-specific instrumentation-gap rows retain honest downgrade coverage.
- 010 is additive, imports only actual retained legacy Changes, preserves original Change APIs/types, and skips IDs already associated with modern Audit Events even when raw migration SQL is retried. No legacy grouping, labels or inverse instructions are manufactured.

## Verification evidence

All HTTP/PostgreSQL/concurrency/migration/restore/runtime tests ran on the authorized cloud host in **new, owned PostgreSQL 16 tmpfs containers**, an internal Docker network, no published ports, synthetic data. Existing production/shared test PostgreSQL and permanent volumes were not used. The runner recorded actual Mounts=tmpfs and empty PortBindings. Local checks were gofmt, go vet, diff checks and the pure logic Audit cursor test; no local service stack ran.

Final source archive SHA-256: **7126a23db16f3ce1177f0cc1fb94146cff08aea15baac13889c827a1b692529e**.
Final cloud log SHA-256: **31f787d982cf0fc0f9bac7f250acca3cabf7fba7126b67bbe4619c6dc5e0658f**.
Restored public-table row-count/ordered-row-JSON digest SHA-256: **45cc95ff595d67a57c187992e73559913e22b2d18c8e0294fc85c64005d8d009**; original/restored files compared equal. Private logs and synthetic backup contents are not committed.

Commands and final outcomes:

| Check | Result |
|---|---|
| go test -race -p 1 -count=1 -v ./... | PASS; PostgreSQL package 196.327s; all G1/G6/G2/G3/G4 normal regressions preserved |
| go vet ./... | PASS locally and in the cloud runner |
| Actual OpenAPI response validation | PASS for Mutation receipt, v1 Audit and legacy import; HTTP routes remain covered |
| TestTableAuditTransactions | PASS: 10 cases, 54.68s |
| TestTableAuditMigrationLegacyImport | PASS: pre-010 import, repeated ApplyMigrations, raw SQL reapply after modern writer, old receipt replay |
| TestAuditApplied010Upgrade | PASS 1.22s; registered original 010, missing 011 returns schema-valid 503, normal migration installs sanitizer, redacts Audit only and preserves identity/replay |
| pg_dump / pg_restore | PASS; all public table row digests equal, then actual old/new binaries operated on restored data |
| TestSelectionLegacyBinaryCompatibility against fixed old G6 | PASS 0.85s; old DTO/query remain valid, new selection parameters rejected as before; new Audit facts from old writer explicitly lack instrumentation |
| Actual new Server stop/start, TestAuditRuntimeRestart seed/verify | PASS 0.19s/0.92s; replay original Operation ID, no duplicate Audit, signed Audit cursor survives restart |
| gofmt / git diff --check | PASS |

The large synthetic test created **10,000 Record events through 20 existing 500-command transactions**, then read **100 pages of 100** with a stable 10,000 count and no duplicates/missing events. Race-instrumented cloud writer time **49.177058524s**, all-page read time **2.785855523s**. These are synthetic QA measurements, not production HTTP percentiles or a release latency guarantee. Audit adds durable write/storage cost; measure representative workloads before production rollout.

Early QA failures were preserved and corrected: SQL variable/column ambiguity; stale migration-count expectations; invalid empty Grid configuration. Self-review also fixed raw migration retry duplication, retained cleanup-generation tracking and uninstrumented converter metadata honesty. Final log has G5_FINAL_QA_PASS and exit **0**. Runtime-specific fixtures are intentionally skipped by the generic suite without explicit owned-runner variables; the actual G6 compatibility and G5 restart fixtures were separately run above. Old G3 binary and Plugin UI integration were **not separately verified** this round. Author review is not an independent review.

## Migration and release safety

010 adds audit_events, audit_coverage, audit_instrumentation_gaps, audit_retention_watermarks, capture/association triggers and a nullable receipt operation_id. Existing business rows are not backfilled with invented history. Audit rows follow configured Change retention; cleanup is bounded and updates its own generation/watermark atomically. Minimal instrumentation-gap/availability facts survive cleanup. Permanent 007 guards are never pruned by Audit cleanup.

011 installs recursive privacy helpers plus an Audit-only BEFORE INSERT/UPDATE OF payload trigger, then scrubs existing Audit copies. This is a separately numbered upgrade: registered original 010 databases skip 010 normally and still receive the privacy fix. Readiness requires 011; missing it yields schema-valid 503 MIGRATION_REQUIRED. Reapplication preserves Event/Operation/Change identity. Redaction is irreversible for Audit storage coordinates; original Record/Attachment/Change values remain unchanged. Forward migration, retry, original-010 upgrade and restored database runtime were verified in owned cloud QA. Keep 011 helpers/sanitizer on downgrade; removing them breaks the new reader and permits private coordinates in Audit.

Future production release requires separate authorization: inspect actual schema 001–006 and current immutable image; confirm stopped-writer window and a complete database plus attachment backup; restore in a new owned environment; explicitly approve the sequence 007 → 008 → 009 → 010 → 011 and its compatibility risks; use one fixed reviewed image; verify health, readiness, Meta and actual gated APIs before resuming writes. Development migration QA does not authorize that sequence on production.

Downgrade: stop all writers; preserve the backup and 007 guards, 008 order triggers and 010 Audit data/receipt column. Drain/disable new Gate queues, invalidate/clear selection scopes under the approved rollout procedure, and avoid mixed old/new writers and their differing locks/protocols. The tested G6 binary accepts the additive schema; old writes are marked as incomplete Actor/typed-cell instrumentation. An old binary is not proof of an uninterrupted fully instrumented Audit epoch. Removing triggers/data or restoring a pre-release backup is a separate, stopped-writer recovery decision; never drop identity guards to force a downgrade. Database restore does not implement Table Rollback.

## Plugin handoff

1. Pin the approved final 40-character Server SHA and explicitly sync OpenAPI 0.8.0; preserve the legacy Change/History decoder and Mutation Queue body/IDs. No Plugin code or API pin changed here.
2. Gate the new reader on deployed tableAudit, migrationRequired=false and readiness. Source/merge are insufficient; production capability is absent.
3. Use GET /v1/tables/{tableId}/audit?category=record with intersecting filters and Server-issued cursor. Keep Actor/Table/filter/request-generation context; discard late responses, restart on filter change or 410, append/dedupe by eventId.
4. Decode payloadVersion=1 typed cells by fieldId and exact operationId/recordId/changeIds. Never associate through timestamp/title/index. The new response header connects a successful or replayed writer to its effects; pre-G5/missing headers are unknown, not a zero-effect proof.
5. Decode payloadVersion=0 and missing metadata as unrecorded. Distinguish missing/null/empty/value and cell-level metadataRecorded from entity snapshot availability. Do not consult current Field/Option config to manufacture event-time facts.
6. Keep complete Table rollback unavailable; Audit/retention does not grant Bulk Mutation or a recoverable checkpoint. Preserve existing Current Cell/range selection and G4 selection semantics.

No unresolved product choice is needed for this implemented read-only scope. Production release risk/window, representative performance and real Plugin integration remain separate tasks. Final CI/normal independent PR review and actual merge status must be checked on GitHub, not inferred from this source evidence.

## Normal independent Review findings

[PR #16](https://github.com/Mahjong404/LoomTable-Server/pull/16) received two actual Devin findings. [Attachment storage coordinates](https://github.com/Mahjong404/LoomTable-Server/pull/16#discussion_r4272560463) were confirmed and fixed at ab2d31e7eb18893ebbadfb2e912ee489902fe66b. Complete HTTP Schema tests cover stored/read/restored/imported payloads and preserved attachment identity; redactedProperties reports the limitation. The independent reviewer confirmed resolution. Legacy Record/Attachment/Change DTOs remain unchanged.

[Already-applied 010 upgrade](https://github.com/Mahjong404/LoomTable-Server/pull/16#discussion_r4272966516) was confirmed: editing registered migration SQL cannot install missing helpers through normal ApplyMigrations. Fixed at 6f0abf1112652b4c1b0e7110b8cee0ede131815f by migration 011, the Audit payload trigger and an explicit readiness requirement. Full cloud Race/restore/old-binary/restart QA was rerun successfully. Review closure and CI must be verified against the latest pushed HEAD; author testing is not independent approval.

After evidence copies, all three owned QA directories, including the final upgrade source/log/synthetic backup, were removed. Final owned container/network/image/volume inventories were empty, and the precise final directory removal exited 0. Production health/ready remained 200/status=ok and Meta retained the version/capabilities above. Production/shared resources and permanent data were preserved.
