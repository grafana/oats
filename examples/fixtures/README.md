# Fixture examples

This directory shows the configuration shape for the two fixtures OATS can
manage itself:

- `compose/rolldice/` — a Docker Compose application and its companion LGTM
  stack.
- `k3d/rolldice/` — an application image and Kubernetes manifests deployed to
  a k3d cluster.

`oats-config.yaml` uses globs to discover both cases. Each case declares its
own fixture, so OATS groups cases by fixture. The cases include the Compose
files, Dockerfile, and Kubernetes manifests they need, making each example
runnable independently.

Run from this directory with:

```sh
oats list
oats --tags compose
oats --tags k3d
```

See [`docs/case-reference.md`](../../docs/case-reference.md) for the complete
fixture configuration. The CI example job runs both fixture cases; the smoke
examples are listed there but require an externally provided app/profile source.
