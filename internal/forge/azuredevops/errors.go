package azuredevops

import (
	"errors"

	azdo "github.com/microsoft/azure-devops-go-api/azuredevops/v7"
)

func isAzureStatus(err error, statusCode int) bool {
	var wrapped azdo.WrappedError
	if !errors.As(err, &wrapped) || wrapped.StatusCode == nil {
		return false
	}
	return *wrapped.StatusCode == statusCode
}
