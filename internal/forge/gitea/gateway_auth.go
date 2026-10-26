package gitea

import (
	"errors"
	"fmt"

	giteagw "go.abhg.dev/gs/internal/gateway/gitea"
)

func newGatewayTokenSource(tok *AuthenticationToken) (giteagw.TokenSource, error) {
	if tok == nil {
		return nil, errors.New("nil authentication token")
	}

	switch tok.AuthType {
	case AuthTypeAPIToken, AuthTypeEnvironmentVariable:
		return giteagw.StaticTokenSource(giteagw.Token{
			Type:  giteagw.TokenTypeToken,
			Value: tok.AccessToken,
		}), nil

	default:
		return nil, fmt.Errorf(
			"no source for authentication type: %v",
			tok.AuthType,
		)
	}
}
