package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.abhg.dev/gs/internal/silog"
)

// InsecureStash is a secrets stash that stores secrets in plain text.
// It prints a warning to stderr the first time it creates the file.
type InsecureStash struct {
	// Destination path to the secrets file.
	Path string // required

	// Log is the logger used by the stash.
	Log *silog.Logger // required
}

var _ Stash = (*InsecureStash)(nil)

type insecureStashData struct {
	// Services is a map from service name to insecureStashService.
	Services map[string]*insecureStashService `json:"services"`
}

func (d *insecureStashData) services() map[string]*insecureStashService {
	if d.Services == nil {
		d.Services = make(map[string]*insecureStashService)
	}
	return d.Services
}

func (d *insecureStashData) empty() bool {
	if len(d.Services) == 0 {
		return true
	}

	for _, svc := range d.Services {
		if !svc.empty() {
			return false
		}
	}

	return true
}

type insecureStashService struct {
	// Secrets is a map from key to insecureStashSecret.
	Secrets map[string]*insecureStashSecret `json:"secrets"`
}

func (s *insecureStashService) secrets() map[string]*insecureStashSecret {
	if s.Secrets == nil {
		s.Secrets = make(map[string]*insecureStashSecret)
	}
	return s.Secrets
}

func (s *insecureStashService) empty() bool {
	return len(s.Secrets) == 0
}

type insecureStashSecret struct {
	Value string `json:"value"`
}

func (f *InsecureStash) load() (*insecureStashData, error) {
	bs, err := os.ReadFile(f.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return new(insecureStashData), nil
		}

		return nil, fmt.Errorf("read: %w", err)
	}

	var data insecureStashData
	if err := json.Unmarshal(bs, &data); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &data, nil
}

func (f *InsecureStash) save(data *insecureStashData) error {
	if data.empty() {
		if err := os.Remove(f.Path); err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove: %w", err)
			}
		}

		return nil
	}

	bs, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	var firstTime bool // whether this is the first time we're writing to the file
	if _, err := os.Stat(f.Path); err != nil {
		firstTime = errors.Is(err, os.ErrNotExist)
	}

	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	if err := os.WriteFile(f.Path, bs, 0o600); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	if firstTime {
		f.Log.Warnf("Storing secrets in plain text at %s. Be careful!", f.Path)
	}

	return nil
}

// SaveSecret stores a secret in the stash.
// The first time it creates the file, it prints a warning to stderr.
func (f *InsecureStash) SaveSecret(service, key, secret string) error {
	data, err := f.load()
	if err != nil {
		return err
	}

	svc, ok := data.services()[service]
	if !ok {
		svc = new(insecureStashService)
		data.services()[service] = svc
	}
	svc.secrets()[key] = &insecureStashSecret{Value: secret}

	return f.save(data)
}

// LoadSecret retrieves a secret from the stash.
// It returns ErrNotFound if the secret does not exist.
func (f *InsecureStash) LoadSecret(service, key string) (string, error) {
	data, err := f.load()
	if err != nil {
		return "", err
	}

	svc, ok := data.services()[service]
	if !ok {
		return "", ErrNotFound
	}

	secret, ok := svc.secrets()[key]
	if !ok {
		return "", ErrNotFound
	}

	return secret.Value, nil
}

// DeleteSecret deletes a secret from the stash.
// It is a no-op if the secret does not exist.
func (f *InsecureStash) DeleteSecret(service, key string) error {
	data, err := f.load()
	if err != nil {
		return err
	}

	if svc, ok := data.services()[service]; ok {
		delete(svc.secrets(), key)
		return f.save(data)
	}

	return nil
}
