#!/usr/bin/env bash
# Demonstrates the filesystem -> OCI path: create a folder and files over
# WebDAV, then launch that folder as a running container inside kind.
#
# Usage: test/scripts/webdav-demo.sh ["message to print from inside the pod"]
set -o errexit
set -o nounset
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
export PATH="${REPO_ROOT}/hack/tools/bin:${PATH}"

CLUSTER_NAME="dist-spec-fs"
REGISTRY_NAME="dist-spec-fs-registry"
REGISTRY_PORT="5001"
REPO="myrepo"
TAG="mytag"
MESSAGE="${1:-hello from a file created over WebDAV}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

echo "==> Writing demo payload"
printf '%s\n' "${MESSAGE}" > "${WORK_DIR}/hello.txt"

# dist-spec-fs archives only the raw files it finds in repo/tag/ - there's no
# base OS image and the generated config sets no Cmd. So the payload must
# include its own tiny static binary to act as the container's entrypoint.
cat > "${WORK_DIR}/main.go" <<'EOF'
package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	data, err := os.ReadFile("/hello.txt")
	if err != nil {
		fmt.Println("failed to read /hello.txt:", err)
		os.Exit(1)
	}
	fmt.Printf("dist-spec-fs kind demo says: %s", data)
	for {
		time.Sleep(time.Hour)
	}
}
EOF

echo "==> Compiling static entrypoint binary"
GOOS=linux GOARCH="$(go env GOARCH)" CGO_ENABLED=0 \
  go build -o "${WORK_DIR}/app" "${WORK_DIR}/main.go"

BASE_URL="http://localhost:${REGISTRY_PORT}/fs"

echo "==> Creating ${REPO}/${TAG} via WebDAV MKCOL"
# MKCOL 405s if the collection already exists from a prior run; that's fine.
curl -fsS -X MKCOL "${BASE_URL}/${REPO}/" -o /dev/null 2>/dev/null || true
curl -fsS -X MKCOL "${BASE_URL}/${REPO}/${TAG}/" -o /dev/null 2>/dev/null || true

echo "==> Uploading files via WebDAV PUT"
curl -fsS -T "${WORK_DIR}/hello.txt" "${BASE_URL}/${REPO}/${TAG}/hello.txt"
curl -fsS -T "${WORK_DIR}/app" "${BASE_URL}/${REPO}/${TAG}/app"

echo "==> Launching ${REPO}:${TAG} as a container in kind"
kubectl --context "kind-${CLUSTER_NAME}" delete pod dist-spec-fs-demo --ignore-not-found --wait=true
kubectl --context "kind-${CLUSTER_NAME}" apply -f "${SCRIPT_DIR}/demo-pod.yaml"
kubectl --context "kind-${CLUSTER_NAME}" wait --for=condition=Ready pod/dist-spec-fs-demo --timeout=120s

echo "==> Pod logs (the WebDAV-uploaded file, printed from inside the kind cluster):"
kubectl --context "kind-${CLUSTER_NAME}" logs dist-spec-fs-demo

# Registries alias a moving "latest" tag onto a fixed one; here that's just a
# symlink on the storage backend (myrepo/latest -> mytag). WebDAV has no verb
# for creating symlinks, so this step goes straight at the underlying
# filesystem via docker exec, same as an operator would on a real disk.
echo "==> Simulating a 'latest' tag: symlinking ${REPO}/latest -> ${TAG} on the storage backend"
docker exec "${REGISTRY_NAME}" ln -sfn "${TAG}" "/data/${REPO}/latest"

echo "==> Launching ${REPO}:latest as a container in kind"
kubectl --context "kind-${CLUSTER_NAME}" delete pod dist-spec-fs-demo-latest --ignore-not-found --wait=true
kubectl --context "kind-${CLUSTER_NAME}" apply -f "${SCRIPT_DIR}/demo-pod-latest.yaml"
kubectl --context "kind-${CLUSTER_NAME}" wait --for=condition=Ready pod/dist-spec-fs-demo-latest --timeout=120s

echo "==> Pod logs (same content, resolved through the 'latest' symlink):"
kubectl --context "kind-${CLUSTER_NAME}" logs dist-spec-fs-demo-latest
