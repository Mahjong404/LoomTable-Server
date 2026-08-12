# Contributing to LoomTable Server

## Branches and pull requests

The default branch is `main`. Start normal work from the current `main` and deliver it through a focused pull request. Use a short, kebab-case branch name with one of these purpose prefixes:

- `feature/` for new capability
- `fix/` for defects
- `docs/` for documentation-only work
- `refactor/` for behavior-preserving restructuring
- `test/` for test-only work
- `chore/` for maintenance and dependency work
- `ci/` for automation changes
- `release/` for a release stabilization branch
- `hotfix/` for an urgent production correction

Examples: `feature/server-p0`, `feature/record-query`, `release/v2.0`, and `hotfix/v2.0.1`.

Use `develop` only if the project deliberately adopts a long-running integration branch; do not create it speculatively. Do not use personal, tool, or automation prefixes such as `agent/`. Keep branches single-purpose and short-lived, merge them through a reviewed pull request, and remove the remote branch after merge.

## Commit and repository hygiene

- Inspect `git status`, ignored files, and the complete diff before staging.
- Stage explicit files that belong to one coherent change; do not blanket-stage a mixed worktree.
- Commit source, tests, maintained documentation, migrations, `go.mod`, `go.sum`, and `.env.example` when they intentionally change.
- Do not commit build output, caches, local data, attachments, dumps, backup archives, logs, editor state, `.env` files, private keys, tokens, personal deployment coordinates, or production data.
- Keep generated or repaired contract files readable and validate them before publishing. Do not accept a diff that turns structured YAML or Markdown into one opaque line.
- Keep SSH targets and private deployment details outside this repository, commits, pull requests, fixtures, and logs.

Before publishing a Server change, run the checks relevant to its scope:

```text
gofmt
go test ./...
go vet ./...
docker compose config --quiet
git diff --check
```

Database, backup, restore, query, and map behavior must also pass their focused integration checks before a pull request is marked ready.
