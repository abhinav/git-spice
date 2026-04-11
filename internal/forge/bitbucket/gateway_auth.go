package bitbucket

import (
	"errors"

	"go.abhg.dev/gs/internal/gateway/bitbucket"
)

func newGatewayTokenSource(tok *AuthenticationToken) (bitbucket.TokenSource, error) {
	if tok == nil {
		return nil, errors.New("nil authentication token")
	}

	return bitbucket.StaticTokenSource(bitbucket.Token{
		AccessToken: tok.AccessToken,
	}), nil
}
