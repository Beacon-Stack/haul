# Contributing to Haul

Thanks for your interest. Here's what you need to know before opening a PR.

## Before you start

- For bug fixes or small improvements, open an issue first so we can discuss whether the fix is in scope and how it should work.
- For new features, open a feature request issue and wait for a response before writing code. We don't want you to spend time on something that won't be merged.
- Check existing issues and PRs — something might already be in progress.

## Development setup

```bash
git clone https://github.com/beacon-stack/haul
cd haul
go build ./...              # confirm Go build is clean
cd web/ui && npm run build  # confirm TypeScript build is clean
```

## Code standards

**Go:**
- `go build ./...` must pass with zero errors
- `go test ./...` must pass (run with `-race` for concurrency-sensitive code)
- Follow existing patterns — read similar files before writing new ones
- New service methods need corresponding tests
- Stall detection, rate tracker, and session wiring are covered by a regression suite — run `make test` before touching any of the files listed in [CLAUDE.md](../CLAUDE.md)

**TypeScript / React:**
- `npm run build` must pass with zero TypeScript errors
- No `any` types without a comment explaining why
- All inline styles use CSS variables (`var(--color-*)`) — no hardcoded colours
- Hover effects via `onMouseEnter`/`onMouseLeave` — matches the existing Shell.tsx pattern
- Every loading state gets a skeleton, not a spinner
- Every error state is handled explicitly — never silently swallowed

**General:**
- Keep changes scoped. Fix the stated problem; don't refactor surrounding code unless it's directly related.
- No feature flags or backwards-compatibility shims — just change the code.
- Add comments where logic isn't self-evident. Skip comments that restate what the code says.

## Pull requests

- Branch from `main`
- One logical change per PR
- Include a clear description of what changed and why
- Reference the issue number if there is one (`Fixes #123`)
- Make sure both `go build ./...` and `cd web/ui && npm run build` pass before submitting
- If you're editing anything in the regression-guarded list in [CLAUDE.md](../CLAUDE.md), `make test` must be clean

## Questions

Open a discussion or an issue. We're happy to help you understand the codebase.
