# Architecture

`saor` is built on a few core principles: zero-copy data whenever possible, statelessness, and leveraging existing enterprise filesystems.

## Core Components

### 1. Storage Backend
The source of truth is always the physical filesystem (e.g., `/mnt/data`). There is no proprietary database for storing artifacts. The folders represent OCI repositories and tags (`/repo/tag/`), and the files within them represent OCI layers/blobs.

### 2. WebDAV Interface (`/fs/`)
We use the standard `golang.org/x/net/webdav` package to serve the storage backend as a WebDAV endpoint. WebDAV is a highly robust and battle-tested protocol for remote file sharing. By exposing the data this way, we inherit native cross-platform support (Windows, macOS, Linux) without writing custom FUSE drivers.

### 3. OCI Interface (`/v2/`)
This layer implements the [OCI Distribution Specification](https://github.com/opencontainers/distribution-spec).
It has two main responsibilities:
- **Manifest Generation (`/v2/<repo>/manifests/<tag>`)**: When a client requests a manifest, the server looks up the directory `<repo>/<tag>`. It packages all files into a temporary `.tar.gz` archive, computes the compressed digest and size, and computes the uncompressed digest (`diff_ids`). It also generates a valid OCI Image Configuration JSON describing the OS and Architecture. Finally, it constructs a valid OCI Image Manifest linking to these blobs.
- **Blob Serving (`/v2/<repo>/blobs/<digest>`)**: When a client requests a blob by its digest, the server uses the `fs.Indexer` to find the corresponding cached `.tar.gz` archive or config JSON and streams it directly to the client using `http.ServeFile`, supporting Range requests automatically.

### 4. File Indexer & Caching
Because OCI clients address blobs by their SHA256 digest, `saor` dynamically archives directories and generates Config JSONs on-the-fly. To prevent hashing and compressing massive directories repeatedly, these generated blobs are stored in a hidden `.cache` directory within the storage root (e.g. `./data/.cache/layers/` and `./data/.cache/configs/`). The indexer maintains an in-memory map of `digest -> path` to quickly serve blobs on subsequent requests. 

## Concurrency and Scaling

The server is largely stateless. The only state maintained in memory is the File Indexer. For multi-node HA deployments, this index would need to be moved to a shared KV store (like Redis or etcd), or the server would need to rely on extended attributes (xattrs) to store the digest directly on the filesystem inodes.
