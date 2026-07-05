// Package secretserver exposes a secret.Stash over local HTTP.
package secretserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	"go.abhg.dev/gs/internal/secret"
)

// Server provides cross-process access to a secret stash.
type Server struct {
	stash secret.Stash
	http  *http.Server
	ln    net.Listener
}

// NewServer starts a local HTTP server backed by the given stash.
func NewServer(stash secret.Stash) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	srv := &Server{
		stash: stash,
		ln:    ln,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/save", srv.save)
	mux.HandleFunc("/load", srv.load)
	mux.HandleFunc("/delete", srv.delete)
	srv.http = &http.Server{Handler: mux}
	go func() {
		_ = srv.http.Serve(ln)
	}()
	return srv, nil
}

// URL returns the server base URL.
func (s *Server) URL() string {
	return "http://" + s.ln.Addr().String()
}

// Close shuts down the secret server.
func (s *Server) Close() error {
	return s.http.Shutdown(context.Background())
}

func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	if err := s.stash.SaveSecret(
		r.FormValue("service"),
		r.FormValue("key"),
		r.FormValue("secret"),
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	value, err := s.stash.LoadSecret(
		r.FormValue("service"),
		r.FormValue("key"),
	)
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

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	if err := s.stash.DeleteSecret(
		r.FormValue("service"),
		r.FormValue("key"),
	); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Client is a secret.Stash backed by a secretserver Server.
type Client struct {
	url *url.URL
}

var _ secret.Stash = (*Client)(nil)

// NewClient builds a client for the given secret server URL.
func NewClient(serverURL string) (*Client, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	return &Client{url: u}, nil
}

// SaveSecret saves a secret in the server stash.
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
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("save secret: %s", resp.Status)
	}

	return nil
}

// LoadSecret loads a secret from the server stash.
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
	defer func() {
		_ = resp.Body.Close()
	}()

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

// DeleteSecret deletes a secret from the server stash.
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
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete secret: %s", resp.Status)
	}

	return nil
}
