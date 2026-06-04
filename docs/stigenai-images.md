# stigenai/* SchemaHero images — build & release runbook

This fork ships two images to **`docker.io/stigenai`** that the infra-blocks
platform deploys via stigen-flux. They are **not** built by the upstream
`tagged-release.yaml` CI (which pushes to the `schemahero` namespace and uses a
tarball-OCI plugin format). The reproducible recipe is
[`scripts/push-stigenai-images.sh`](../scripts/push-stigenai-images.sh).

| Image | What | Format | Tag scheme |
|-------|------|--------|------------|
| `stigenai/plugin-postgres:<TAG>-<arch>` | the postgres planner plugin | bare-binary ORAS artifact, per-arch (`-amd64`/`-arm64`), layer title `schemahero-postgres` | bare integer (`:1` … `:6`) |
| `stigenai/schemahero-manager:<MTAG>` | the operator + per-database controller | multiarch image (`deploy/Dockerfile.multiarch` target `manager`); **bundles no plugin** | `0.24.0-stigen.N` |

The controller (`{db}-controller` StatefulSet) runs the **manager** image and
**oras-downloads** `plugin-postgres:<tag>-<arch>` to `~/.schemahero/plugins/` at
startup — so the plugin image must exist for the tag the controller is told to use.

## When to cut a new tag

- **Touched `plugins/postgres/`** → new **plugin** tag. (Canonicalization /
  introspection / DDL rendering all live plugin-side.)
- **Touched `pkg/apis/` or the manager** → new **manager** tag. The CR schema
  crosses a gob boundary AND the structural CRD, so a new spec **field** also
  requires re-vendoring the CRD (below).
- A plugin-only change does **not** need a manager bump; a `pkg/apis` field change
  needs **both** (the plugin and manager share `pkg/apis`).

## Build & push

```sh
docker login                              # push access to docker.io/stigenai
make manifests                            # only if pkg/apis changed (regenerates CRDs)

./scripts/push-stigenai-images.sh plugin  7
./scripts/push-stigenai-images.sh manager 0.24.0-stigen.3
# or both:
./scripts/push-stigenai-images.sh both    7 0.24.0-stigen.3
```

Verify:
```sh
oras manifest fetch docker.io/stigenai/plugin-postgres:7-amd64
docker manifest inspect docker.io/stigenai/schemahero-manager:0.24.0-stigen.3
```

## Required follow-up in stigen-flux (the deploy is GitOps)

Edit `infrastructure/schemahero/helmrelease.yaml` and bump to match what you pushed:

- plugin only: `- "--plugin-tag=<TAG>"`
- manager (and/or new CRD field): `image.tag: "<MTAG>"` **and** `- "--manager-tag=<MTAG>"`
- new `pkg/apis` field: also copy the regenerated
  `config/crds/v1/schemas.schemahero.io_tables.yaml` into
  `infrastructure/schemahero/crds/schemas.schemahero.io_tables.yaml` (the chart runs
  `crds: Skip`; Flux SSA-applies the vendored CRDs — a stale CRD prunes the new field).

The operator forwards `--plugin-tag`/`--manager-tag` to **every** `{db}-controller`
StatefulSet via `buildDatabaseControllerArgs`, so all cells roll together.

## History

| plugin | adds |
|--------|------|
| `:0` | legacy prod plugin |
| `:1`/`:2` | extended fork: advanced indexes, CHECK/EXCLUDE, functions, triggers, RLS; comma-spacing fix |
| `:3` | `::text[]` array-cast canonicalization (varchar CHECK) |
| `:4` | index-column **opClass** + ordered mixed column+expression indexes (needs manager `0.24.0-stigen.2`) |
| `:5` | column-default `::type` cast canonicalization |
| `:6` | `stripOIDClass` keeps string-literal quotes (completes the SET DEFAULT churn fix) |

| manager | adds |
|---------|------|
| `0.24.0-stigen.1` | extended-fields planner (forwarded as `--manager-tag`) |
| `0.24.0-stigen.2` | `opClass` field on `PostgresqlTableIndexColumn` (pkg/apis) + re-vendored CRD |
