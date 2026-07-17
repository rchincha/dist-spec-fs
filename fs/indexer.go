package fs

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BlobInfo holds metadata about a generated OCI blob (layer or config).
type BlobInfo struct {
	Digest string
	Size   int64
	Path   string
}

// Indexer manages the conversion of directories into OCI Container Images
// by creating and caching .tar.gz archives and JSON configs.
type Indexer struct {
	rootDir      string
	cacheDir     string
	mu           sync.RWMutex
	digestToPath map[string]string // Maps "sha256:abcd..." to cached physical file
}

// NewIndexer creates a new concurrent-safe Indexer and ensures cache directories exist.
func NewIndexer(rootDir string) (*Indexer, error) {
	cacheDir := filepath.Join(rootDir, ".cache")
	if err := os.MkdirAll(filepath.Join(cacheDir, "layers"), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(cacheDir, "configs"), 0755); err != nil {
		return nil, err
	}

	return &Indexer{
		rootDir:      rootDir,
		cacheDir:     cacheDir,
		digestToPath: make(map[string]string),
	}, nil
}

// ArchiveDirectory compresses a directory into a .tar.gz layer, computes its diff_id
// (uncompressed digest), and generates a corresponding OCI Image Config JSON.
func (i *Indexer) ArchiveDirectory(repo, tag string) (config BlobInfo, layer BlobInfo, err error) {
	dirPath := filepath.Join(i.rootDir, repo, tag)

	// Read directory contents
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}

	// Create temporary layer file
	tempLayerPath := filepath.Join(i.cacheDir, "layers", fmt.Sprintf("temp_%d.tar.gz", time.Now().UnixNano()))
	layerFile, err := os.Create(tempLayerPath)
	if err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}
	defer os.Remove(tempLayerPath) // Will fail cleanly if we successfully rename it later

	// Setup hashers
	compressedHasher := sha256.New()
	uncompressedHasher := sha256.New()

	// compressedWriter writes to both the hasher and the file
	compressedWriter := io.MultiWriter(compressedHasher, layerFile)
	gzWriter := gzip.NewWriter(compressedWriter)

	// uncompressedWriter writes to both the uncompressed hasher and the gzip compressor
	uncompressedWriter := io.MultiWriter(uncompressedHasher, gzWriter)
	tarWriter := tar.NewWriter(uncompressedWriter)

	var layerSize int64

	for _, entry := range entries {
		if entry.IsDir() {
			continue // skip subdirs for MVP
		}

		filePath := filepath.Join(dirPath, entry.Name())
		info, err := os.Stat(filePath)
		if err != nil {
			return BlobInfo{}, BlobInfo{}, err
		}

		header, err := tar.FileInfoHeader(info, info.Name())
		if err != nil {
			return BlobInfo{}, BlobInfo{}, err
		}
		header.Name = entry.Name() // ensure relative path in tar

		if err := tarWriter.WriteHeader(header); err != nil {
			return BlobInfo{}, BlobInfo{}, err
		}

		file, err := os.Open(filePath)
		if err != nil {
			return BlobInfo{}, BlobInfo{}, err
		}

		if _, err := io.Copy(tarWriter, file); err != nil {
			file.Close()
			return BlobInfo{}, BlobInfo{}, err
		}
		file.Close()
	}

	if err := tarWriter.Close(); err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}
	if err := gzWriter.Close(); err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}

	// Compute digests
	compressedDigest := "sha256:" + hex.EncodeToString(compressedHasher.Sum(nil))
	uncompressedDigest := "sha256:" + hex.EncodeToString(uncompressedHasher.Sum(nil))

	// Get final layer size
	stat, err := layerFile.Stat()
	if err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}
	layerSize = stat.Size()
	layerFile.Close() // Close before rename

	// Move layer to final cached name
	finalLayerPath := filepath.Join(i.cacheDir, "layers", compressedDigest+".tar.gz")
	os.Rename(tempLayerPath, finalLayerPath)

	layer = BlobInfo{
		Digest: compressedDigest,
		Size:   layerSize,
		Path:   finalLayerPath,
	}

	// --- Generate OCI Config ---
	configData := map[string]interface{}{
		"architecture": "amd64",
		"os":           "linux",
		"rootfs": map[string]interface{}{
			"type": "layers",
			"diff_ids": []string{
				uncompressedDigest,
			},
		},
		"created": time.Now().UTC().Format(time.RFC3339),
	}

	configBytes, err := json.Marshal(configData)
	if err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}

	configHasher := sha256.New()
	configHasher.Write(configBytes)
	configDigest := "sha256:" + hex.EncodeToString(configHasher.Sum(nil))
	configSize := int64(len(configBytes))

	finalConfigPath := filepath.Join(i.cacheDir, "configs", configDigest+".json")
	if err := os.WriteFile(finalConfigPath, configBytes, 0644); err != nil {
		return BlobInfo{}, BlobInfo{}, err
	}

	config = BlobInfo{
		Digest: configDigest,
		Size:   configSize,
		Path:   finalConfigPath,
	}

	// Safely store mappings in index
	i.mu.Lock()
	i.digestToPath[layer.Digest] = layer.Path
	i.digestToPath[config.Digest] = config.Path
	i.mu.Unlock()

	return config, layer, nil
}

// GetPath looks up the physical cached file path for a given OCI blob digest.
func (i *Indexer) GetPath(digest string) (string, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	path, ok := i.digestToPath[digest]
	return path, ok
}

// ExtractImage extracts the given layer digests (which must exist in the cache as tar.gz files)
// into the physical directory for the repo and tag.
func (i *Indexer) ExtractImage(repo, tag string, layerDigests []string) error {
	dirPath := filepath.Join(i.rootDir, repo, tag)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return err
	}

	for _, digest := range layerDigests {
		path, ok := i.GetPath(digest)
		if !ok {
			// Try to guess path based on cache layout
			cachePath := filepath.Join(i.cacheDir, "layers", digest+".tar.gz")
			if _, err := os.Stat(cachePath); err == nil {
				path = cachePath
			} else {
				return fmt.Errorf("layer %s not found in cache", digest)
			}
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		gzReader, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return err
		}

		tarReader := tar.NewReader(gzReader)
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				gzReader.Close()
				f.Close()
				return err
			}

			targetPath := filepath.Join(dirPath, header.Name)

			// Security: reject entries that would extract outside dirPath
			// (e.g. "../../etc/passwd" or an absolute path), regardless of
			// how filepath.Join normalizes them.
			if !isWithinDir(targetPath, dirPath) {
				continue
			}

			switch header.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(targetPath, 0755); err != nil {
					return err
				}
			case tar.TypeReg:
				// Ensure parent directory exists
				if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
					return err
				}

				outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
				if err != nil {
					return err
				}
				if _, err := io.Copy(outFile, tarReader); err != nil {
					outFile.Close()
					return err
				}
				outFile.Close()
			}
		}
		gzReader.Close()
		f.Close()
	}

	return nil
}

// isWithinDir reports whether target is dir itself or a descendant of it,
// guarding against tar entries (e.g. "../../etc/passwd" or absolute paths)
// that would otherwise extract outside the target directory.
func isWithinDir(target, dir string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// SaveBlob saves a raw blob to the cache and indexes it.
func (i *Indexer) SaveBlob(digest string, reader io.Reader) error {
	tempPath := filepath.Join(i.cacheDir, "layers", fmt.Sprintf("upload_%d.tmp", time.Now().UnixNano()))
	f, err := os.Create(tempPath)
	if err != nil {
		return err
	}
	defer os.Remove(tempPath)

	hasher := sha256.New()
	writer := io.MultiWriter(f, hasher)

	if _, err := io.Copy(writer, reader); err != nil {
		f.Close()
		return err
	}
	f.Close()

	computedDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if digest != computedDigest {
		return fmt.Errorf("digest mismatch: expected %s, got %s", digest, computedDigest)
	}

	finalPath := filepath.Join(i.cacheDir, "layers", digest+".tar.gz") // We assume it's a tar.gz for layers, configs will just have this extension too but it doesn't matter for storage
	if err := os.Rename(tempPath, finalPath); err != nil {
		return err
	}

	i.mu.Lock()
	i.digestToPath[digest] = finalPath
	i.mu.Unlock()

	return nil
}

// GetCachePath returns the physical path in the cache for a given digest.
func (i *Indexer) GetCachePath(digest string) string {
	// We assume layers/configs are mixed or we just check both.
	// For simplicity, layer blobs are in layers/ and config blobs in configs/
	// Since both are accessed by digest, checking both is easy.
	p := filepath.Join(i.cacheDir, "layers", digest+".tar.gz")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	p = filepath.Join(i.cacheDir, "configs", digest+".json")
	return p
}
