package shamhub

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/must"
	"go.abhg.dev/gs/internal/secret"
)

type loginRequest struct {
	Username string `json:"username,omitempty"`
}

type loginResponse struct {
	Token string `json:"token,omitempty"`
}

var _ = shamhubHandler("POST /login", (*ShamHub).handleLogin)

func (sh *ShamHub) handleLogin(w http.ResponseWriter, r *http.Request) {
	var data loginRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&data); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var buf [8]byte
	_, _ = rand.Read(buf[:])
	token := fmt.Sprintf("%x", buf[:])

	sh.mu.Lock()
	defer sh.mu.Unlock()

	for _, u := range sh.users {
		if u.Username != data.Username {
			continue
		}

		sh.tokens[token] = u.Username
		res := loginResponse{Token: token}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(res); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	http.Error(w, "user not found", http.StatusNotFound)
}

type shamUser struct {
	Username string
}

// RegisterUser registers a new user against the Forge
// with the given username and password.
func (sh *ShamHub) RegisterUser(username string) error {
	sh.mu.Lock()
	defer sh.mu.Unlock()

	for _, u := range sh.users {
		if u.Username == username {
			return fmt.Errorf("user %q already exists", username)
		}
	}

	sh.users = append(sh.users, shamUser{Username: username})
	return nil
}

// AuthenticationToken defines the token returned by the ShamHub forge.
type AuthenticationToken struct {
	forge.AuthenticationToken

	tok string
}

// AuthenticationFlow initiates the authentication flow for the ShamHub forge.
// The flow is optimized for ease of use from test scripts
// and is not representative of a real-world authentication flow.
//
// To authenticate, the user must set the SHAMHUB_USERNAME environment variable
// before attempting to authenticate.
// The flow will fail if these variables are not set.
// The flow will also fail if the user is already authenticated.
func (f *Forge) AuthenticationFlow(ctx context.Context) (forge.AuthenticationToken, error) {
	must.NotBeBlankf(f.APIURL, "API URL is required")

	username := os.Getenv("SHAMHUB_USERNAME")
	if username == "" {
		return nil, errors.New("SHAMHUB_USERNAME is required")
	}

	loginURL, err := url.JoinPath(f.APIURL, "/login")
	if err != nil {
		return nil, fmt.Errorf("parse API URL: %w", err)
	}

	req := loginRequest{
		Username: username,
	}
	var res loginResponse
	if err := f.jsonHTTPClient().Post(ctx, loginURL, req, &res); err != nil {
		return nil, fmt.Errorf("login: %w", err)
	}

	return &AuthenticationToken{tok: res.Token}, nil
}

func (f *Forge) secretService() string {
	must.NotBeBlankf(f.URL, "URL is required")
	return "shamhub:" + f.URL
}

// SaveAuthenticationToken saves the given authentication token to the stash.
func (f *Forge) SaveAuthenticationToken(stash secret.Stash, t forge.AuthenticationToken) error {
	return stash.SaveSecret(f.secretService(), "token", t.(*AuthenticationToken).tok)
}

// LoadAuthenticationToken loads the authentication token from the stash.
func (f *Forge) LoadAuthenticationToken(stash secret.Stash) (forge.AuthenticationToken, error) {
	token, err := stash.LoadSecret(f.secretService(), "token")
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}
	return &AuthenticationToken{tok: token}, nil
}

// ClearAuthenticationToken removes the authentication token from the stash.
func (f *Forge) ClearAuthenticationToken(stash secret.Stash) error {
	return stash.DeleteSecret(f.secretService(), "token")
}
