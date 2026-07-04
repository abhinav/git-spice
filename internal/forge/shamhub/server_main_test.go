package shamhub

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerEnv(t *testing.T) {
	env := serverEnv{
		APIURL:     "http://127.0.0.1:1",
		GitURL:     "http://127.0.0.1:2",
		AdminToken: "admin-token",
		GitRoot:    "/tmp/shamhub-git",
		SecretURL:  "http://127.0.0.1:3",
	}

	assert.Equal(t, ""+
		"export SHAMHUB_API_URL=\"http://127.0.0.1:1\"\n"+
		"export SHAMHUB_URL=\"http://127.0.0.1:2\"\n"+
		"export SHAMHUB_ADMIN_TOKEN=\"admin-token\"\n"+
		"export SHAMHUB_GIT_ROOT=\"/tmp/shamhub-git\"\n"+
		"export SHAMHUB_SECRET_URL=\"http://127.0.0.1:3\"\n",
		env.shell())

	bs, err := json.Marshal(&env)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"apiUrl": "http://127.0.0.1:1",
		"gitUrl": "http://127.0.0.1:2",
		"adminToken": "admin-token",
		"gitRoot": "/tmp/shamhub-git",
		"secretUrl": "http://127.0.0.1:3"
	}`, string(bs))
}
