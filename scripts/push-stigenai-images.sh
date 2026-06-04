#!/usr/bin/env bash
#
# Build + push the stigenai/* SchemaHero images that the infra-blocks platform
# (via stigen-flux) actually deploys. These are NOT produced by the upstream
# `tagged-release.yaml` CI — that pushes to the `schemahero` Docker Hub namespace
# and uses a tarball-OCI plugin format. This script is the single in-repo source
# of truth for rebuilding the stigenai images, in the exact format the deployed
# manager expects.
#
#   Plugin:  docker.io/stigenai/plugin-postgres:<TAG>-<arch>   (bare-binary ORAS
#            artifact; the manager's plugin-downloader pulls per-arch at startup
#            and expects the layer title "schemahero-postgres")
#   Manager: docker.io/stigenai/schemahero-manager:<MTAG>      (multiarch image;
#            bundles NO plugin — it oras-downloads the plugin at runtime)
#
# Tag scheme:
#   - plugin tag is a bare integer (:1, :2, … :6 as of 2026-06-03). Bump on ANY
#     change under plugins/postgres/.
#   - manager tag is 0.24.0-stigen.N. Bump on ANY change under pkg/apis/ or the
#     manager build. A new pkg/apis FIELD also requires re-vendoring the CRD
#     (`make manifests` → copy config/crds/v1/schemas.schemahero.io_tables.yaml
#     into stigen-flux infrastructure/schemahero/crds/).
#
# After pushing, you MUST bump stigen-flux infrastructure/schemahero/helmrelease.yaml:
#   --plugin-tag=<TAG>   and/or   image.tag: "<MTAG>" + --manager-tag=<MTAG>
#
# Usage (run `docker login` first with push access to docker.io/stigenai):
#   ./scripts/push-stigenai-images.sh plugin  6
#   ./scripts/push-stigenai-images.sh manager 0.24.0-stigen.2
#   ./scripts/push-stigenai-images.sh both    6 0.24.0-stigen.2
#
set -euo pipefail

NAMESPACE="docker.io/stigenai"
ARCHES=(amd64 arm64)
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

red() { printf '\033[0;31m%s\033[0m\n' "$*"; }
grn() { printf '\033[0;32m%s\033[0m\n' "$*"; }

need() { command -v "$1" >/dev/null 2>&1 || { red "missing required tool: $1"; exit 1; }; }

push_plugin() {
  local tag="$1"
  [ -n "$tag" ] || { red "plugin tag required (e.g. 6)"; exit 1; }
  need go; need oras
  for arch in "${ARCHES[@]}"; do
    local d; d="$(mktemp -d)"
    grn "building schemahero-postgres linux/${arch} (plugin tag ${tag})"
    ( cd "${REPO_ROOT}/plugins/postgres" \
        && CGO_ENABLED=0 GOOS=linux GOARCH="${arch}" \
           go build -ldflags="-s -w -X main.version=${tag}" -o "${d}/schemahero-postgres" . )
    grn "oras push ${NAMESPACE}/plugin-postgres:${tag}-${arch}"
    # The bare filename is load-bearing: it becomes the OCI layer title
    # "schemahero-postgres", which the manager's plugin-downloader looks for.
    ( cd "${d}" && oras push "${NAMESPACE}/plugin-postgres:${tag}-${arch}" schemahero-postgres )
    rm -rf "${d}"
  done
  grn "plugin-postgres:${tag} pushed for: ${ARCHES[*]}"
}

push_manager() {
  local mtag="$1"
  [ -n "$mtag" ] || { red "manager tag required (e.g. 0.24.0-stigen.2)"; exit 1; }
  need docker
  docker buildx version >/dev/null 2>&1 || { red "docker buildx required (multiarch)"; exit 1; }
  grn "buildx multiarch schemahero-manager:${mtag} (linux/amd64,linux/arm64)"
  # --target manager builds bin/manager (CGO_ENABLED=0) and packages it; the image
  # bundles no plugin (the controller oras-downloads it at runtime).
  docker buildx build \
    --platform linux/amd64,linux/arm64 \
    -f "${REPO_ROOT}/deploy/Dockerfile.multiarch" --target manager \
    -t "${NAMESPACE}/schemahero-manager:${mtag}" --push "${REPO_ROOT}"
  grn "schemahero-manager:${mtag} pushed (multiarch)"
}

case "${1:-}" in
  plugin)  push_plugin "${2:-}";;
  manager) push_manager "${2:-}";;
  both)    push_plugin "${2:-}"; push_manager "${3:-}";;
  *) red "usage: $0 {plugin <TAG>|manager <MTAG>|both <TAG> <MTAG>}"; exit 2;;
esac
