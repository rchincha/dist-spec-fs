package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/saor/fs"
	"github.com/saor/oci"
	"github.com/saor/webdav"
)

var Version = "dev"

func main() {
	var rootDir string
	var port string
	var showVersion bool

	// Define command-line flags to configure the server's storage location and port.
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.StringVar(&rootDir, "root", "./data", "Root directory for the filesystem (where native files and folders reside)")
	flag.StringVar(&port, "port", "8080", "Port to run the server on")
	flag.Parse()

	if showVersion {
		fmt.Printf("saor version %s\n", Version)
		os.Exit(0)
	}

	// Ensure the root directory exists on startup so WebDAV and OCI don't fail immediately on an empty instance.
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		log.Fatalf("failed to create root directory %s: %v", rootDir, err)
	}

	// Initialize the file indexer. This indexer generates tar.gz caches
	// for directories and maps OCI blob digests to them.
	indexer, err := fs.NewIndexer(rootDir)
	if err != nil {
		log.Fatalf("failed to initialize indexer: %v", err)
	}

	// Create a new HTTP multiplexer to route requests to either WebDAV or OCI.
	mux := http.NewServeMux()

	// 1. WebDAV Handler
	// We mount the WebDAV interface under the "/fs/" prefix.
	// Users can connect to this using standard OS network drive tools (Windows, Mac, Linux).
	// Writes to this endpoint go directly to the 'rootDir' on the physical filesystem.
	webdavHandler := webdav.NewHandler(rootDir, "/fs")
	mux.Handle("/fs/", webdavHandler)

	// 2. OCI Server Handler
	// We mount the OCI Distribution Specification interface under the "/v2/" prefix.
	// This handles standard container registry requests (manifests and blobs) dynamically.
	ociServer := oci.NewServer(rootDir, indexer)
	mux.Handle("/v2/", ociServer)

	// Log startup configuration for debugging and user visibility.
	log.Printf("Starting saor server on :%s", port)
	log.Printf("Root Directory: %s", rootDir)
	log.Printf("WebDAV endpoint: http://localhost:%s/fs/", port)
	log.Printf("OCI dist-spec endpoint: http://localhost:%s/v2/", port)

	// Start the HTTP server.
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
