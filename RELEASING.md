# Releasing

Releases are automated with `release-please` and GoReleaser.

## One-time setup

1. Enable immutable releases in GitHub repository settings.
2. Make sure releases are merged through the `main` branch.
3. Use Conventional Commits for releasable changes (`feat:`, `fix:`, `feat!:`).

## Normal release flow

1. Merge changes into `main`.
2. Wait for `release-please` to open or update the release PR.
3. Review and merge the release PR.
4. The `Release` workflow will then:
   - create a draft GitHub release and tag
   - build binaries with GoReleaser
   - upload release assets and `checksums.txt`
   - create GitHub artifact attestations for the assets
   - publish the draft release

The release is only immutable after publication, so the workflow intentionally
uploads assets and attestations before publishing.

`CHANGELOG.md` remains the hand-written upgrade guide. Automated release notes
are written to `RELEASES.md` by `release-please`.
