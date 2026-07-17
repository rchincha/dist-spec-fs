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

@test "Upload via WebDAV, pull via skopeo, push via skopeo, and verify" {
    # 1. Create a directory structure natively or via WebDAV
    run curl -s -X MKCOL http://localhost:${PORT}/fs/myrepo/
    [ "$status" -eq 0 ]
    run curl -s -X MKCOL http://localhost:${PORT}/fs/myrepo/mytag/
    [ "$status" -eq 0 ]
    
    # 2. Upload a file via WebDAV (PUT)
    echo -n "hello world" > "${BATS_TEST_TMPDIR}/file.txt"
    run curl -s -T "${BATS_TEST_TMPDIR}/file.txt" http://localhost:${PORT}/fs/myrepo/mytag/file.txt
    [ "$status" -eq 0 ]
    
    # 3. Pull using skopeo to verify it is a valid container image
    run skopeo copy --src-tls-verify=false --dest-tls-verify=false docker://localhost:${PORT}/myrepo:mytag oci:${BATS_TEST_TMPDIR}/skopeo_dest
    [ "$status" -eq 0 ]
    [ -f "${BATS_TEST_TMPDIR}/skopeo_dest/index.json" ]
    
    # 4. Push back to the server under a new tag
    run skopeo copy --src-tls-verify=false --dest-tls-verify=false oci:${BATS_TEST_TMPDIR}/skopeo_dest docker://localhost:${PORT}/myrepo:pushedtag
    [ "$status" -eq 0 ]
    
    # 5. Verify the file exists natively in the filesystem and is untarred
    [ -f "${ROOT_DIR}/myrepo/pushedtag/file.txt" ]
    
    # 6. Verify the file content
    content=$(cat "${ROOT_DIR}/myrepo/pushedtag/file.txt")
    [ "$content" = "hello world" ]
}
