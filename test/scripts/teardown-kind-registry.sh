#!/usr/bin/env bash
# Tears down everything setup-kind-registry.sh created.
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
export PATH="${REPO_ROOT}/hack/tools/bin:${PATH}"

CLUSTER_NAME="dist-spec-fs"
REGISTRY_NAME="dist-spec-fs-registry"

echo "==> Deleting kind cluster '${CLUSTER_NAME}'"
kind delete cluster --name "${CLUSTER_NAME}" || true

echo "==> Removing registry container '${REGISTRY_NAME}'"
docker rm -f "${REGISTRY_NAME}" >/dev/null 2>&1 || true

echo "==> Removing registry data volume"
docker volume rm "${REGISTRY_NAME}-data" >/dev/null 2>&1 || true
