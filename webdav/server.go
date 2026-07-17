package webdav

import (
	"golang.org/x/net/webdav"
	"net/http"
)

// NewHandler creates a new HTTP handler that serves the WebDAV protocol.
// WebDAV is a standard protocol for remote filesystem access, supported natively
// by Windows, macOS, and Linux without needing third-party drivers.
func NewHandler(rootDir string, prefix string) http.Handler {
	// webdav.Dir configures the handler to use the local filesystem starting at rootDir.
	fs := webdav.Dir(rootDir)
	
	// webdav.NewMemLS configures an in-memory lock system. 
	// WebDAV supports file locking to prevent concurrent edits. For a stateless MVP,
	// an in-memory lock system is sufficient, though production deployments might 
	// need a persistent lock system if running multiple instances of this server.
	ls := webdav.NewMemLS()

	handler := &webdav.Handler{
		// Prefix determines the HTTP path prefix where the WebDAV server is mounted.
		// For example, if Prefix is "/fs", a request to "/fs/myrepo/file.txt" will
		// map to "myrepo/file.txt" inside rootDir.
		Prefix:     prefix,
		FileSystem: fs,
		LockSystem: ls,
		Logger: func(r *http.Request, err error) {
			// A production system should hook into a structured logger here to
			// trace WebDAV operations and authentication failures.
			if err != nil {
				// e.g. log.Printf("WEBDAV [%s]: %s, ERROR: %v", r.Method, r.URL, err)
			}
		},
	}
	
	return handler
}
