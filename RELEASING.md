# Releasing

Releases are automated with `release-please` and GoReleaser.

## One-time setup

1. Enable immutable releases in GitHub repository settings.
2. Make sure releases are merged through the `main` branch.
3. Use squash merges so the squash commit message uses the PR title.
4. Enforce semantic PR titles with `.github/workflows/pr-title.yml`.
5. Use Conventional Commit prefixes for releasable changes, such as:
   - `feat:`
   - `fix:`
   - `feat!:` / `fix!:` for breaking changes
   - `chore:` / `docs:` / `test:` for non-release changes

## Normal release flow

1. Merge changes into `main`.
2. Wait for `release-please` to open or update the release PR.
3. Review and squash-merge the release PR.
4. The `Release` workflow will then:
   - create a draft GitHub release and tag
   - build binaries with GoReleaser
   - upload release assets and `checksums.txt`
   - create GitHub artifact attestations for the assets
   - publish the draft release

## Republish an existing tag

Use `workflow_dispatch` with an existing `tag_name` to rebuild assets and
publish that draft release without running `release-please` again. Run the
workflow from that same tag, or the workflow guard will fail before publishing.

The release is only immutable after publication, so the workflow intentionally
uploads assets and attestations before publishing.

`CHANGELOG.md` is managed by `release-please`. Hand-written migration and
breaking-change notes live in `UPGRADING.md`.
