# Contributing to LoomTable Server

## Trunk-based workflow

Read [Git workflow](docs/agent-ops/git-workflow.md) before branching, committing, reviewing, merging, releasing, changing CI, or cleaning up. It owns branch names, author identity, Issue/PR selection, squash-merge defaults, tags/releases, notifications, and safe cleanup.

Only `main` is long-lived. Use short-lived `feat/`, `fix/`, `refactor/`, `docs/`, `test/`, or `chore/` task branches. Existing historical names remain exceptions until their active PRs and worktrees are retired. CI and product changes require a PR; validated tiny documentation or behavior-preserving maintenance may go directly to main when repository rules permit.

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
