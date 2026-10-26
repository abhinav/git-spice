package azuredevops

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.abhg.dev/gs/internal/secret"
	"go.abhg.dev/gs/internal/silog"
	"go.abhg.dev/gs/internal/ui"
	"go.abhg.dev/gs/internal/xec"
	"go.abhg.dev/gs/internal/xec/xectest"
	"go.uber.org/mock/gomock"
)

func TestAuthType_MarshalText(t *testing.T) {
	tests := []struct {
		name    string
		give    AuthType
		want    string
		wantErr string
	}{
		{name: "PAT", give: AuthTypePAT, want: "pat"},
		{name: "AzureCLI", give: AuthTypeAzureCLI, want: "azure-cli"},
		{
			name:    "EnvironmentVariable",
			give:    AuthTypeEnvironmentVariable,
			wantErr: "should never save AuthTypeEnvironmentVariable",
		},
		{name: "Unknown", give: AuthType(999), wantErr: "unknown auth type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.give.MarshalText()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestAuthType_UnmarshalText(t *testing.T) {
	tests := []struct {
		name    string
		give    string
		want    AuthType
		wantErr string
	}{
		{name: "PAT", give: "pat", want: AuthTypePAT},
		{name: "AzureCLI", give: "azure-cli", want: AuthTypeAzureCLI},
		{name: "Unknown", give: "unknown", wantErr: "unknown auth type"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got AuthType
			err := got.UnmarshalText([]byte(tt.give))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAuthType_String(t *testing.T) {
	tests := []struct {
		give AuthType
		want string
	}{
		{AuthTypePAT, "Personal Access Token"},
		{AuthTypeAzureCLI, "Azure CLI"},
		{AuthTypeEnvironmentVariable, "Environment Variable"},
		{AuthType(999), "AuthType(999)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.give.String())
		})
	}
}

func TestAuthHasAzureDevOpsPAT(t *testing.T) {
	var logBuffer bytes.Buffer
	f := Forge{
		Options: Options{
			Token: "token",
		},
		Log: silog.New(&logBuffer, nil),
	}

	view := ui.NewFileView(io.Discard)

	t.Run("AuthenticationFlow", func(t *testing.T) {
		_, err := f.AuthenticationFlow(t.Context(), view)
		require.Error(t, err)
		assert.ErrorContains(t, err, "already authenticated")
		assert.Contains(t, logBuffer.String(), "Already authenticated")
	})

	t.Run("LoadAndSave", func(t *testing.T) {
		var stash secret.MemoryStash
		tok, err := f.LoadAuthenticationToken(&stash)
		require.NoError(t, err)

		err = f.SaveAuthenticationToken(&stash, tok)
		require.NoError(t, err)

		got, err := f.LoadAuthenticationToken(&stash)
		require.NoError(t, err)

		assert.Equal(t, tok, got)

		require.NoError(t, f.ClearAuthenticationToken(&stash))
	})
}

func TestSaveAuthenticationToken_validation(t *testing.T) {
	f := Forge{Log: silog.Nop()}

	t.Run("EmptyAccessToken", func(t *testing.T) {
		var stash secret.MemoryStash
		err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
			AuthType:    AuthTypePAT,
			AccessToken: "",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "access token is required")
	})

	t.Run("EnvironmentVariable", func(t *testing.T) {
		var stash secret.MemoryStash
		err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
			AuthType:    AuthTypeEnvironmentVariable,
			AccessToken: "token",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "should never save AuthTypeEnvironmentVariable")
	})

	t.Run("UnknownAuthType", func(t *testing.T) {
		var stash secret.MemoryStash
		err := f.SaveAuthenticationToken(&stash, &AuthenticationToken{
			AuthType:    AuthType(999),
			AccessToken: "token",
		})
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown auth type")
	})
}

func TestLoadAuthenticationToken(t *testing.T) {
	f := Forge{Log: silog.Nop()}

	t.Run("ValidToken", func(t *testing.T) {
		var stash secret.MemoryStash
		require.NoError(t, stash.SaveSecret(
			f.URL(), "token",
			`{"auth_type":"pat","access_token":"my-token"}`,
		))

		tok, err := f.LoadAuthenticationToken(&stash)
		require.NoError(t, err)

		at := tok.(*AuthenticationToken)
		assert.Equal(t, "my-token", at.AccessToken)
		assert.Equal(t, AuthTypePAT, at.AuthType)
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		var stash secret.MemoryStash
		require.NoError(t, stash.SaveSecret(f.URL(), "token", "not-json"))

		_, err := f.LoadAuthenticationToken(&stash)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unmarshal token")
	})
}

func TestCLIAuthenticator(t *testing.T) {
	discardView := ui.NewFileView(io.Discard)

	t.Run("Success", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		execer := xectest.NewMockExecer(mockCtrl)

		// First call: az account show
		execer.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		// Second call: az account get-access-token
		execer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				// Write the token to stdout.
				_, _ = cmd.Stdout.Write([]byte("my-az-token\n"))
				return nil
			})

		tok, err := (&CLIAuthenticator{
			AZ:     "az",
			execer: execer,
		}).Authenticate(t.Context(), discardView)
		require.NoError(t, err)

		assert.Equal(t, "my-az-token", tok.AccessToken)
		assert.Equal(t, AuthTypeAzureCLI, tok.AuthType)
	})

	t.Run("NotAuthenticated", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		execer := xectest.NewMockExecer(mockCtrl)

		execer.EXPECT().
			Run(gomock.Any()).
			Return(&xec.ExitError{
				Stderr: []byte("Please run 'az login'"),
			})

		_, err := (&CLIAuthenticator{
			AZ:     "az",
			execer: execer,
		}).Authenticate(t.Context(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "not authenticated")
		assert.ErrorContains(t, err, "Please run 'az login'")
	})

	t.Run("AccountShowOtherError", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		execer := xectest.NewMockExecer(mockCtrl)

		execer.EXPECT().
			Run(gomock.Any()).
			Return(errors.New("az not found"))

		_, err := (&CLIAuthenticator{
			AZ:     "az",
			execer: execer,
		}).Authenticate(t.Context(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "az not found")
	})

	t.Run("GetTokenFailed", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		execer := xectest.NewMockExecer(mockCtrl)

		// First call succeeds.
		execer.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		// Second call fails.
		execer.EXPECT().
			Run(gomock.Any()).
			Return(&xec.ExitError{
				Stderr: []byte("resource not consented"),
			})

		_, err := (&CLIAuthenticator{
			AZ:     "az",
			execer: execer,
		}).Authenticate(t.Context(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to get Azure DevOps access token")
		assert.ErrorContains(t, err, "resource not consented")
	})

	t.Run("EmptyToken", func(t *testing.T) {
		mockCtrl := gomock.NewController(t)
		execer := xectest.NewMockExecer(mockCtrl)

		execer.EXPECT().
			Run(gomock.Any()).
			Return(nil)

		execer.EXPECT().
			Run(gomock.Any()).
			DoAndReturn(func(cmd *exec.Cmd) error {
				// Write empty to stdout.
				_, _ = cmd.Stdout.Write([]byte(""))
				return nil
			})

		_, err := (&CLIAuthenticator{
			AZ:     "az",
			execer: execer,
		}).Authenticate(t.Context(), discardView)
		require.Error(t, err)
		assert.ErrorContains(t, err, "no access token returned")
	})
}
