package secret

import (
	"encoding/json"
	"fmt"
	"os"
)

// SimpleStore is a simple secret store.
//
// Secrets are stored in a protected file, in JSON format, in $XDG_DATA_HOME
type SimpleStore struct {
	location string
}

var _ Stash = (*SimpleStore)(nil)

type SimpleData struct {
	Services map[string]map[string]string `json:"services"`
}

// SaveSecret saves a secret in the keyring.
func (s *SimpleStore) SaveSecret(service, key, secret string) error {
	d, err := s.readData()
	if err != nil {
		return err
	}

	d.Services[service][key] = secret
	return s.writeData(d)
}

// LoadSecret loads a secret from the keyring.
func (s *SimpleStore) LoadSecret(service, key string) (string, error) {
	d, err := s.readData()
	if err != nil {
		return "", err
	}

	return d.Services[service][key], nil
}

// DeleteSecret deletes a secret from the keyring.
func (s *SimpleStore) DeleteSecret(service, key string) error {
	d, err := s.readData()
	if err != nil {
		return err
	}

	delete(d.Services[service], key)
	return s.writeData(d)
}

func (s *SimpleStore) readData() (SimpleData, error) {
	bytes, err := os.ReadFile(s.location)
	if err != nil {
		if os.IsNotExist(err) {
			return SimpleData{}, nil
		}
		return SimpleData{}, fmt.Errorf("couldn't read store: %s", err)
	}

	var d SimpleData
	if err := json.Unmarshal(bytes, &d); err != nil {
		return SimpleData{}, fmt.Errorf("error reading json: %s", err)
	}
	return d, nil
}

func (s *SimpleStore) writeData(d SimpleData) error {
	bytes, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("couldn't marshal json: %s", err)
	}

	return os.WriteFile(s.location, bytes, 0600)
}
