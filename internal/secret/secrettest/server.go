// Package secrettest provides a cross-process testable secret.Stash.
package secrettest

import (
	"testing"

	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/secret/secretserver"
)

// Server is a test server for secret.Stash.
type Server struct {
	http *secretserver.Server
}

// NewServer creates a new server for a secret stash.
// It will automatically shut down when the test ends.
func NewServer(t testing.TB) *Server {
	t.Helper()

	srv, err := secretserver.NewServer(new(secret.MemoryStash))
	if err != nil {
		t.Fatalf("create secret server: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Logf("close secret server: %v", err)
		}
	})
	return &Server{http: srv}
}

// URL returns the URL at which the server is listening.
// Use [Client] to talk to this server.
func (s *Server) URL() string {
	return s.http.URL()
}

// Client is a client for a secret stash server.
type Client = secretserver.Client

// NewClient creates a new client
// capable of talking to a secret stash server.
//
// The server URL should be the base URL of the server.
func NewClient(srvURL string) (*Client, error) {
	return secretserver.NewClient(srvURL)
}
