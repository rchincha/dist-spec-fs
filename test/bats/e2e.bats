#!/usr/bin/env bats

setup() {
    export ROOT_DIR="${BATS_TEST_TMPDIR}/data"
    export PORT=8082
    mkdir -p "$ROOT_DIR"
    
    go build -o dist-spec-fs .
    
    ./dist-spec-fs --root "$ROOT_DIR" --port "$PORT" > "${BATS_TEST_TMPDIR}/server.log" 2>&1 &
    export SERVER_PID=$!
    
    sleep 1
}

teardown() {
    if [ -n "$SERVER_PID" ]; then
        kill "$SERVER_PID" || true
    fi
}

@test "OCI root endpoint returns 200 OK" {
    run curl -s -o /dev/null -w "%{http_code}" http://localhost:${PORT}/v2/
    [ "$status" -eq 0 ]
    [ "$output" = "200" ]
}

@test "Upload file via WebDAV and pull via skopeo as valid container image" {
    # 1. Create a directory structure natively or via WebDAV
    run curl -s -X MKCOL http://localhost:${PORT}/fs/myrepo/
    [ "$status" -eq 0 ]
    run curl -s -X MKCOL http://localhost:${PORT}/fs/myrepo/mytag/
    [ "$status" -eq 0 ]
    
    # 2. Upload a file via WebDAV (PUT)
    echo -n "hello world" > "${BATS_TEST_TMPDIR}/file.txt"
    run curl -s -T "${BATS_TEST_TMPDIR}/file.txt" http://localhost:${PORT}/fs/myrepo/mytag/file.txt
    [ "$status" -eq 0 ]
    
    # 3. Pull using skopeo to verify it is a valid container image!
    # skopeo will request the manifest, which triggers the server to tar.gz the folder
    # and generate a valid OCI config JSON, returning a standard Image Manifest.
    run skopeo copy --src-tls-verify=false --dest-tls-verify=false docker://localhost:${PORT}/myrepo:mytag oci:${BATS_TEST_TMPDIR}/skopeo_dest
    
    # Check if skopeo succeeded
    [ "$status" -eq 0 ]
    
    # Verify the layout exists
    [ -f "${BATS_TEST_TMPDIR}/skopeo_dest/index.json" ]
}

@test "Accessing an unknown blob returns 404" {
    run curl -s -o /dev/null -w "%{http_code}" http://localhost:${PORT}/v2/myrepo/blobs/sha256:0000000000000000000000000000000000000000000000000000000000000000
    [ "$status" -eq 0 ]
    [ "$output" = "404" ]
}
