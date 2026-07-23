package oci

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/saor/fs"
)

// Server handles HTTP requests matching the OCI Distribution Specification.
type Server struct {
	rootDir string
	indexer *fs.Indexer
}

// NewServer creates a new OCI Dist-Spec server.
func NewServer(rootDir string, indexer *fs.Indexer) *Server {
	return &Server{
		rootDir: rootDir,
		indexer: indexer,
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")

	if path == "/v2/" || path == "/v2" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if strings.Contains(path, "/manifests/") {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			s.handleManifestsGet(w, r)
			return
		}
		if r.Method == http.MethodPut {
			s.handleManifestsPut(w, r)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if strings.Contains(path, "/blobs/uploads/") {
		if r.Method == http.MethodPost {
			s.handleBlobUploadPost(w, r)
			return
		}
		if r.Method == http.MethodPut {
			s.handleBlobUploadPut(w, r)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if strings.Contains(path, "/blobs/") {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			s.handleBlobsGet(w, r)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleManifestsGet(w http.ResponseWriter, r *http.Request) {
	repo, ref := extractRepoAndTag(r.URL.Path, "manifests")

	// Clients that resolve a tag to its digest (e.g. containerd's remotes/docker
	// resolver) re-fetch manifest content by digest rather than by tag. Since
	// manifests are generated on the fly rather than stored under repo/tag/,
	// serve previously generated content straight from the digest cache here.
	if strings.HasPrefix(ref, "sha256:") {
		data, ok := s.indexer.GetManifest(ref)
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Header().Set("Docker-Content-Digest", ref)
		if r.Method == http.MethodGet {
			w.Write(data)
		}
		return
	}
	tag := ref

	configBlob, layerBlob, err := s.indexer.ArchiveDirectory(repo, tag)
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`))
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type layer struct {
		MediaType   string            `json:"mediaType"`
		Digest      string            `json:"digest"`
		Size        int64             `json:"size"`
		Annotations map[string]string `json:"annotations,omitempty"`
	}

	manifest := struct {
		SchemaVersion int     `json:"schemaVersion"`
		MediaType     string  `json:"mediaType"`
		Config        layer   `json:"config"`
		Layers        []layer `json:"layers"`
	}{
		SchemaVersion: 2,
		MediaType:     "application/vnd.oci.image.manifest.v1+json",
		Config: layer{
			MediaType: "application/vnd.oci.image.config.v1+json",
			Digest:    configBlob.Digest,
			Size:      configBlob.Size,
		},
		Layers: []layer{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    layerBlob.Digest,
				Size:      layerBlob.Size,
				Annotations: map[string]string{
					// Lets generic OCI artifact clients (e.g. oras pull) know
					// what filename to write the layer blob to; without it
					// they skip the layer entirely.
					"org.opencontainers.image.title": tag + ".tar.gz",
				},
			},
		},
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	manifestSum := sha256.Sum256(manifestBytes)
	manifestDigest := "sha256:" + hex.EncodeToString(manifestSum[:])
	s.indexer.SaveManifest(manifestDigest, manifestBytes)

	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Docker-Content-Digest", manifestDigest)
	if r.Method == http.MethodGet {
		w.Write(manifestBytes)
	}
}

func (s *Server) handleBlobsGet(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	digest := parts[len(parts)-1]

	path, ok := s.indexer.GetPath(digest)
	if !ok {
		// Attempt to resolve directly from cache
		// This is useful if a push happened but the in-memory index was lost due to restart
		cachePath := s.indexer.GetCachePath(digest)
		if _, err := os.Stat(cachePath); err == nil {
			path = cachePath
		} else {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[{"code":"BLOB_UNKNOWN","message":"blob unknown"}]}`))
			return
		}
	}

	http.ServeFile(w, r, path)
}

func (s *Server) handleBlobUploadPost(w http.ResponseWriter, r *http.Request) {
	repo := extractRepo(r.URL.Path, "blobs")

	session, err := newUploadSession()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/uploads/%s", repo, session))
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

// newUploadSession returns a random session ID for a blob upload.
// Each POST gets its own ID so Location headers don't collide across
// concurrent pushes; the ID isn't otherwise tracked since uploads are
// monolithic single PUTs identified by their digest.
func newUploadSession() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) handleBlobUploadPut(w http.ResponseWriter, r *http.Request) {
	digest := r.URL.Query().Get("digest")
	if digest == "" {
		http.Error(w, "digest query parameter is required", http.StatusBadRequest)
		return
	}

	if err := s.indexer.SaveBlob(digest, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	repo := extractRepo(r.URL.Path, "blobs")
	w.Header().Set("Location", fmt.Sprintf("/v2/%s/blobs/%s", repo, digest))
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleManifestsPut(w http.ResponseWriter, r *http.Request) {
	repo, tag := extractRepoAndTag(r.URL.Path, "manifests")

	var manifest struct {
		Layers []struct {
			Digest string `json:"digest"`
		} `json:"layers"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.Unmarshal(body, &manifest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var layerDigests []string
	for _, l := range manifest.Layers {
		layerDigests = append(layerDigests, l.Digest)
	}

	if err := s.indexer.ExtractImage(repo, tag, layerDigests); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/v2/%s/manifests/%s", repo, tag))
	w.WriteHeader(http.StatusCreated)
}

// Helpers
func extractRepo(path, marker string) string {
	parts := strings.Split(path, "/")
	start := 0
	end := 0
	for i, p := range parts {
		if p == "v2" {
			start = i + 1
		} else if p == marker {
			end = i
			break
		}
	}
	if start >= end {
		return ""
	}
	return strings.Join(parts[start:end], "/")
}

func extractRepoAndTag(path, marker string) (string, string) {
	parts := strings.Split(path, "/")
	start := 0
	end := 0
	for i, p := range parts {
		if p == "v2" {
			start = i + 1
		} else if p == marker {
			end = i
			break
		}
	}
	if start >= end {
		return "", ""
	}
	repo := strings.Join(parts[start:end], "/")
	tag := parts[len(parts)-1]
	return repo, tag
}
