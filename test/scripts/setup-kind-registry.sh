#!/usr/bin/env bash
# Stands up a kind cluster that pulls images from dist-spec-fs itself,
# following the pattern in https://kind.sigs.k8s.io/docs/user/local-registry/
# — except the "registry" is dist-spec-fs, so anything written to its
# WebDAV endpoint is immediately pullable as an OCI image from inside kind.
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
export PATH="${REPO_ROOT}/hack/tools/bin:${PATH}"

CLUSTER_NAME="dist-spec-fs"
REGISTRY_NAME="dist-spec-fs-registry"
REGISTRY_PORT="5001"
IMAGE_TAG="dist-spec-fs:kind"

echo "==> Building ${IMAGE_TAG}"
docker build -t "${IMAGE_TAG}" "${REPO_ROOT}"

echo "==> Starting ${REGISTRY_NAME} (dist-spec-fs serving WebDAV + the OCI registry)"
if [ "$(docker inspect -f '{{.State.Running}}' "${REGISTRY_NAME}" 2>/dev/null || true)" != 'true' ]; then
  docker run -d --restart=always \
    -p "127.0.0.1:${REGISTRY_PORT}:8080" \
    --network bridge --name "${REGISTRY_NAME}" \
    -v "${REGISTRY_NAME}-data:/data" \
    "${IMAGE_TAG}"
else
  echo "    already running"
fi

echo "==> Creating kind cluster '${CLUSTER_NAME}'"
if ! kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"; then
  kind create cluster --config "${SCRIPT_DIR}/kind-config.yaml"
else
  echo "    already exists"
fi

echo "==> Wiring node containerd: localhost:${REGISTRY_PORT} -> http://${REGISTRY_NAME}:8080"
REGISTRY_DIR="/etc/containerd/certs.d/localhost:${REGISTRY_PORT}"
for node in $(kind get nodes --name "${CLUSTER_NAME}"); do
  docker exec "${node}" mkdir -p "${REGISTRY_DIR}"
  cat <<EOF | docker exec -i "${node}" cp /dev/stdin "${REGISTRY_DIR}/hosts.toml"
[host."http://${REGISTRY_NAME}:8080"]
EOF
done

echo "==> Connecting ${REGISTRY_NAME} to the kind docker network"
if [ "$(docker inspect -f='{{json .NetworkSettings.Networks.kind}}' "${REGISTRY_NAME}")" = 'null' ]; then
  docker network connect kind "${REGISTRY_NAME}"
fi

echo "==> Documenting the registry (kube-public/local-registry-hosting)"
cat <<EOF | kubectl --context "kind-${CLUSTER_NAME}" apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: local-registry-hosting
  namespace: kube-public
data:
  localRegistryHosting.v1: |
    host: "localhost:${REGISTRY_PORT}"
    help: "https://kind.sigs.k8s.io/docs/user/local-registry/"
EOF

echo "==> Ready."
echo "    WebDAV: http://localhost:${REGISTRY_PORT}/fs/"
echo "    OCI:    http://localhost:${REGISTRY_PORT}/v2/"
