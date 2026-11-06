package azuredevops

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/microsoft/azure-devops-go-api/azuredevops/v7"
)

// ErrNotFound matches an operation that could not find its requested resource.
var ErrNotFound = errors.New("not found")

func normalizeError(err error) error {
	if err == nil {
		return nil
	}

	var responseError azuredevops.WrappedError
	if errors.As(err, &responseError) &&
		responseError.StatusCode != nil &&
		*responseError.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	return err
}
