package github

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/gateway/github"
)

type testGatewayTokenSource struct{}

func (testGatewayTokenSource) Token(context.Context) (string, error) {
	return "test-token", nil
}

func newTestGateway(t *testing.T, apiURL string) *github.Gateway {
	client, err := github.NewGateway(apiURL, nil, testGatewayTokenSource{})
	require.NoError(t, err)
	return client
}

func TestMain(m *testing.M) {
	name := filepath.Base(os.Args[0])
	if runtime.GOOS == "windows" {
		name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	}

	if name == "git" {
		if len(os.Args) == 3 && os.Args[1] == "credential" && os.Args[2] == "fill" {
			fmt.Print(`protocol=https
host=github.com
username=test-user
password=test-token
`)
			os.Exit(0)
		}

		os.Exit(1)
	}

	os.Exit(m.Run())
}
