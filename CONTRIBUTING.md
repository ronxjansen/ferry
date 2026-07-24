# Contributing

## Commit messages

This repo uses [Conventional Commits](https://www.conventionalcommits.org/). Releases are fully automated by [release-please](https://github.com/googleapis/release-please), which reads commit messages on `master` to decide the next version and generate the changelog:

- `feat: ...` — new feature, bumps the **minor** version
- `fix: ...` — bug fix, bumps the **patch** version
- `feat!: ...` or a `BREAKING CHANGE:` footer — bumps the **major** version
- `docs:`, `chore:`, `refactor:`, `test:`, `ci:` — no release, hidden from the changelog

PRs are squash-merged, so the **PR title** must follow the convention (enforced by the `pr-title` workflow). Individual commits within a PR can be messy.

## Release flow

1. Merge PRs into `master` as usual.
2. release-please maintains a "release PR" that accumulates changes, bumps `cmd/root.go` and `.release-please-manifest.json`, and updates `CHANGELOG.md`.
3. Merging that release PR creates the git tag and GitHub release.
4. GoReleaser then builds `linux`/`darwin` (`amd64`/`arm64`) binaries and attaches them to the release.

Never bump the version or tag manually — release-please owns both.
