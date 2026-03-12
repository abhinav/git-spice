package gitlab

import (
	"context"
	"errors"
	"fmt"

	"go.abhg.dev/gs/internal/gateway/gitlab"
)

func newGatewayTokenSource(
	tok *AuthenticationToken,
) (gitlab.TokenSource, error) {
	if tok == nil {
		return nil, errors.New("nil authentication token")
	}

	switch tok.AuthType {
	case AuthTypePAT, AuthTypeEnvironmentVariable:
		return gitlab.StaticTokenSource(gitlab.Token{
			Type:  gitlab.TokenTypePrivateToken,
			Value: tok.AccessToken,
		}), nil

	case AuthTypeOAuth2:
		return gitlab.StaticTokenSource(gitlab.Token{
			Type:  gitlab.TokenTypeBearer,
			Value: tok.AccessToken,
		}), nil

	case AuthTypeGitLabCLI:
		return &cliGatewayTokenSource{
			hostname: tok.Hostname,
			cli:      newGitLabCLI(""),
		}, nil

	default:
		return nil, fmt.Errorf(
			"no source for authentication type: %v",
			tok.AuthType,
		)
	}
}

type cliGatewayTokenSource struct {
	hostname string
	cli      gitlabCLI
}

func (s *cliGatewayTokenSource) Token(
	ctx context.Context,
) (gitlab.Token, error) {
	token, err := s.cli.Token(ctx, s.hostname)
	if err != nil {
		return gitlab.Token{},
			fmt.Errorf("get token from GitLab CLI: %w", err)
	}

	return gitlab.Token{
		Type:  gitlab.TokenTypeBearer,
		Value: token,
	}, nil
}
