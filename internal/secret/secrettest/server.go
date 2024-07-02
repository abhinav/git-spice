// Package secrettest provides a cross-process testable secret.Stash.
package secrettest

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"go.abhg.dev/gs/internal/secret"
)

// Server is a test server for secret.Stash.
type Server struct {
	t    testing.TB
	mem  secret.MemoryStash
	http *httptest.Server
}

// NewServer creates a new server for a secret stash.
// It will automatically shut down when the test ends.
func NewServer(t testing.TB) *Server {
	srv := Server{t: t}

	mux := http.NewServeMux()
	mux.HandleFunc("/save", srv.save)
	mux.HandleFunc("/load", srv.load)
	mux.HandleFunc("/delete", srv.delete)

	srv.http = httptest.NewServer(mux)
	t.Cleanup(srv.http.Close)
	return &srv
}

// URL returns the URL at which the server is listening.
// Use [Client] to talk to this server.
func (s *Server) URL() string {
	return s.http.URL
}

// save is the HTTP handler for saving a secret.
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	service := r.FormValue("service")
	key := r.FormValue("key")
	secret := r.FormValue("secret")
	s.t.Logf("[secret] save(%q, %q, ***)", service, key)

	err := s.mem.SaveSecret(service, key, secret)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// load is the HTTP handler for loading a secret.
func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	service := r.FormValue("service")
	key := r.FormValue("key")
	s.t.Logf("[secret] load(%q, %q)", service, key)

	value, err := s.mem.LoadSecret(service, key)
	if err != nil {
		if errors.Is(err, secret.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, _ = io.WriteString(w, value)
}

// delete is the HTTP handler for deleting a secret.
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	service := r.FormValue("service")
	key := r.FormValue("key")
	s.t.Logf("[secret] delete(%q, %q)", service, key)

	err := s.mem.DeleteSecret(service, key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Client is a client for a secret stash server.
// It is safe for concurrent use.
type Client struct {
	url *url.URL
}

var _ secret.Stash = (*Client)(nil)

// NewClient creates a new client
// capable of talking to a secret stash server.
//
// The server URL should be the base URL of the server.
func NewClient(srvURL string) (*Client, error) {
	u, err := url.Parse(srvURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	return &Client{url: u}, nil
}

// SaveSecret saves a secret in the stash.
func (c *Client) SaveSecret(service, key, secret string) error {
	q := url.Values{
		"service": []string{service},
		"key":     []string{key},
		"secret":  []string{secret},
	}
	u := c.url.JoinPath("/save")

	resp, err := http.PostForm(u.String(), q)
	if err != nil {
		return fmt.Errorf("save secret: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("save secret: %s", resp.Status)
	}

	return nil
}

// LoadSecret loads a secret from the stash.
func (c *Client) LoadSecret(service, key string) (string, error) {
	q := url.Values{
		"service": []string{service},
		"key":     []string{key},
	}
	u := c.url.JoinPath("/load")
	u.RawQuery = q.Encode()

	resp, err := http.Get(u.String())
	if err != nil {
		return "", fmt.Errorf("load secret: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return "", secret.ErrNotFound
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("load secret: %s", resp.Status)
	}

	secret, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("load secret: %w", err)
	}

	return string(secret), nil
}

// DeleteSecret deletes a secret from the stash.
func (c *Client) DeleteSecret(service, key string) error {
	q := url.Values{
		"service": []string{service},
		"key":     []string{key},
	}
	u := c.url.JoinPath("/delete")

	resp, err := http.PostForm(u.String(), q)
	if err != nil {
		return fmt.Errorf("delete secret: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete secret: %s", resp.Status)
	}

	return nil
}
