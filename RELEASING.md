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
4. After `release-please` creates a release, the `Release` workflow's
   `goreleaser` job checks out the new tag, builds binaries, uploads release
   assets and `checksums.txt`, creates GitHub artifact attestations, publishes
   a signed Packslip manifest linked to those attestations, and publishes the
   draft release.

Linux release artifacts are built with `CGO_ENABLED=0`, so the Linux archives
work on both glibc- and musl-based distributions.

## Republish an existing tag

Use `workflow_dispatch` with an existing `tag_name` to run the separate
`publish-existing-tag` job. It rebuilds assets, creates attestations, publishes
a signed Packslip manifest linked to those attestations, and publishes that
draft release without running `release-please` again. Run the workflow from
that same tag, or the workflow guard will fail before publishing.

The release is only immutable after publication, so the workflow intentionally
uploads assets and attestations before publishing.

`CHANGELOG.md` is managed by `release-please`. Hand-written migration and
breaking-change notes live in `UPGRADING.md`.

## Packslip resources

Each binary archive contains `share/oats/oats.usage.kdl` and the agent skill at
`share/oats/skills/oats/SKILL.md`. The signed Packslip manifest declares these
archive resources; consumers do not need to execute OATS or fetch a moving
repository branch to obtain them. The action links the existing build
attestations rather than publishing duplicate provenance.

Packslip 1.2 infers `libc: gnu` for Linux archives and cannot clear just that
constraint through its generator. The binaries remain libc-independent, but
Packslip-based selection on musl is not yet supported by this integration.
Download the Linux archive directly on musl until the generator supports an
unrestricted libc without also removing the OS and architecture constraints.

Validate archive layouts with `goreleaser release --snapshot --clean --skip=publish`
(with `GCX_VERSION` from `scripts/gcx-version.sh`). Inspect the generated manifest
with `packslip show`; inspection alone is not signature verification. Release CI
signs using its OIDC identity and verifies the bundle before uploading, all
before the draft becomes immutable. Local snapshot builds do not publish or
exercise OIDC signing.
