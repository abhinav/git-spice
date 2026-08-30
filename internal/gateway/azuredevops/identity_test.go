package azuredevops

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/identity"
	"github.com/microsoft/azure-devops-go-api/azuredevops/v7/location"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGateway_CurrentUserID(t *testing.T) {
	want := uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")
	client := NewMockLocationClient(gomock.NewController(t))
	client.EXPECT().GetConnectionData(
		gomock.Any(),
		location.GetConnectionDataArgs{},
	).Return(&location.ConnectionData{
		AuthenticatedUser: &identity.Identity{Id: &want},
	}, nil)

	gateway := &Gateway{locationClient: client}
	got, err := gateway.CurrentUserID(t.Context())

	require.NoError(t, err)
	assert.Equal(t, want.String(), got)
}

func TestGateway_CurrentUserID_missingAuthenticatedUser(t *testing.T) {
	client := NewMockLocationClient(gomock.NewController(t))
	client.EXPECT().GetConnectionData(
		gomock.Any(),
		location.GetConnectionDataArgs{},
	).Return(&location.ConnectionData{}, nil)

	gateway := &Gateway{locationClient: client}
	_, err := gateway.CurrentUserID(t.Context())

	assert.EqualError(t, err, "connection data has no authenticated user")
}

func TestGateway_ReviewerID_fallsBackToGeneralSearch(t *testing.T) {
	want := uuid.MustParse("01234567-89ab-cdef-0123-456789abcdef")
	client := NewMockIdentityClient(gomock.NewController(t))
	client.EXPECT().ReadIdentities(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args identity.ReadIdentitiesArgs) (*[]identity.Identity, error) {
			require.Equal(t, "MailAddress", *args.SearchFilter)
			require.Equal(t, "octo@example.com", *args.FilterValue)
			return &[]identity.Identity{}, nil
		},
	)
	client.EXPECT().ReadIdentities(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, args identity.ReadIdentitiesArgs) (*[]identity.Identity, error) {
			require.Equal(t, "General", *args.SearchFilter)
			require.Equal(t, "octo@example.com", *args.FilterValue)
			return &[]identity.Identity{{Id: &want}}, nil
		},
	)

	gateway := &Gateway{identityClient: client}
	got, err := gateway.ReviewerID(t.Context(), "octo@example.com")

	require.NoError(t, err)
	assert.Equal(t, want.String(), got)
}
