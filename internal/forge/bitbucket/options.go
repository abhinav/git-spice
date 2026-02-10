// Package bitbucket provides a wrapper around Bitbucket's APIs
// in a manner compliant with the [forge.Forge] interface.
package bitbucket

// DefaultURL is the default URL for Bitbucket Cloud.
const DefaultURL = "https://bitbucket.org"

// DefaultAPIURL is the default URL for Bitbucket Cloud API.
const DefaultAPIURL = "https://api.bitbucket.org/2.0"

// Options defines command line options for the Bitbucket Forge.
// These are all hidden in the CLI,
// and are expected to be set only via environment variables.
type Options struct {
	// URL is the URL for Bitbucket.
	// Override this for testing or self-hosted Bitbucket instances.
	URL string `name:"bitbucket-url" hidden:"" config:"forge.bitbucket.url" env:"BITBUCKET_URL" help:"Base URL for Bitbucket web requests"`

	// APIURL is the URL for Bitbucket's API.
	// Override this for testing or self-hosted Bitbucket instances.
	APIURL string `name:"bitbucket-api-url" hidden:"" config:"forge.bitbucket.apiURL" env:"BITBUCKET_API_URL" help:"Base URL for Bitbucket API requests"`

	// Token is a fixed token used to authenticate with Bitbucket.
	// This may be used to skip the login flow.
	Token string `name:"bitbucket-token" hidden:"" env:"BITBUCKET_TOKEN" help:"Bitbucket API token"`
}
