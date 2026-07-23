#!/usr/bin/env bats
# End-to-end: create a folder/files via WebDAV and confirm they launch as a
# running container inside a real kind cluster. Heavy (spins up a kind
# cluster and downloads a node image), so it only runs when explicitly
# requested via RUN_KIND_TESTS=1 (see `make kind-test`).

setup() {
    REPO_ROOT="$(cd "${BATS_TEST_DIRNAME}/../.." && pwd)"
    export PATH="${REPO_ROOT}/hack/tools/bin:${PATH}"

    if [ "${RUN_KIND_TESTS:-}" != "1" ]; then
        skip "set RUN_KIND_TESTS=1 to run the kind + WebDAV e2e test (needs docker; make kind-test)"
    fi
    if ! command -v docker >/dev/null 2>&1; then
        skip "docker not available"
    fi
}

teardown() {
    if [ "${RUN_KIND_TESTS:-}" = "1" ]; then
        # Surfaced only when the test failed (bats suppresses stdout for
        # passing tests) - gives CI logs something to debug from instead of
        # a bare "status != 0" with no context.
        echo "==> Debug: pods"
        kubectl --context "kind-saor" get pods -A -o wide 2>&1 || true
        echo "==> Debug: registry container log (tail)"
        docker logs --tail=100 saor-registry 2>&1 || true
        echo "==> Debug: docker ps"
        docker ps -a 2>&1 || true

        "${REPO_ROOT}/test/scripts/teardown-kind-registry.sh" || true
    fi
}

@test "create files via WebDAV and launch them as a container in kind" {
    run "${REPO_ROOT}/test/scripts/setup-kind-registry.sh"
    echo "$output"
    [ "$status" -eq 0 ]

    run "${REPO_ROOT}/test/scripts/webdav-demo.sh" "hello from bats"
    echo "$output"
    [ "$status" -eq 0 ]
    [[ "$output" == *"hello from bats"* ]]

    run kubectl --context "kind-saor" get pod saor-demo -o jsonpath='{.status.phase}'
    echo "$output"
    [ "$status" -eq 0 ]
    [ "$output" = "Running" ]

    # webdav-demo.sh also symlinks myrepo/latest -> mytag on the storage
    # backend and launches a second pod from the "latest" tag as proof the
    # symlink resolves to the same content.
    run kubectl --context "kind-saor" get pod saor-demo-latest -o jsonpath='{.status.phase}'
    echo "$output"
    [ "$status" -eq 0 ]
    [ "$output" = "Running" ]

    run kubectl --context "kind-saor" logs saor-demo-latest
    echo "$output"
    [ "$status" -eq 0 ]
    [[ "$output" == *"hello from bats"* ]]
}
