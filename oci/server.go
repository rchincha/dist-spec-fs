package oci

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/dist-spec-fs/fs"
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

	if path == "/v2/" || path == "/v2" {
		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
		w.WriteHeader(http.StatusOK)
		return
	}

	if strings.Contains(path, "/manifests/") {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			s.handleManifests(w, r)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	if strings.Contains(path, "/blobs/") {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			s.handleBlobs(w, r)
			return
		}
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) handleManifests(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	v2Idx := -1
	manifestsIdx := -1
	for i, p := range parts {
		if p == "v2" {
			v2Idx = i
		} else if p == "manifests" {
			manifestsIdx = i
		}
	}

	if v2Idx == -1 || manifestsIdx == -1 || manifestsIdx <= v2Idx+1 || manifestsIdx == len(parts)-1 {
		http.Error(w, "invalid manifest path", http.StatusBadRequest)
		return
	}

	repo := strings.Join(parts[v2Idx+1:manifestsIdx], "/")
	tag := parts[len(parts)-1]

	// Dynamically generate the container image layers and config
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
			},
		},
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if r.Method == http.MethodGet {
		w.Write(manifestBytes)
	}
}

func (s *Server) handleBlobs(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	digest := parts[len(parts)-1]

	path, ok := s.indexer.GetPath(digest)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"errors":[{"code":"BLOB_UNKNOWN","message":"blob unknown"}]}`))
		return
	}

	http.ServeFile(w, r, path)
}
