//go:build shamhub

package main

import (
	"fmt"
	"os"

	"go.abhg.dev/gs/internal/forge"
	"go.abhg.dev/gs/internal/forge/shamhub"
	"go.abhg.dev/gs/internal/secret/secretserver"
	"go.abhg.dev/gs/internal/silog"
)

func init() {
	if err := configureShamHubSecretStash(os.Getenv); err != nil {
		panic(err)
	}

	_extraForges = append(_extraForges, func(log *silog.Logger) forge.Forge {
		return &shamhub.Forge{Log: log}
	})
}

func configureShamHubSecretStash(getenv func(string) string) error {
	if secretURL := getenv("SHAMHUB_SECRET_URL"); secretURL != "" {
		secretStash, err := secretserver.NewClient(secretURL)
		if err != nil {
			return fmt.Errorf("create ShamHub secret client: %w", err)
		}
		_keyringStash = secretStash
	}
	return nil
}
