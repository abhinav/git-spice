# shamhub

shamhub is a fake, GitHub-like Forge used by git-spice for testing.

It provides an on-demand server-side component
that exposes a REST API similar to (but not the same as) GitHub.
It also provides a command line tool (`shamhub`)
that is used inside test scripts to interact with the server.

## Adding server-side functionality

To add server-side functionality to shamhub,
declare JSON-serializable request and response structs.

```go
type editChangeRequest struct {
    // ...
}

type editChangeResponse struct{
    // ...
}
```

Request fields can use struct tags to specify how values are extracted:

- `path:"name"` - extract from URL path (always required)
- `form:"name"` or `query:"name"` - extract from form data (optional unless tagged with `,required`)
- `json:"name"` - extract from JSON body

See shamhubRESTHandler for details on tags.

Determine the HTTP handler pattern that will handle the request.
Any `path:".."` tags in the request struct
must be present in the pattern.

```
PATCH /{owner}/{repo}/change/{number}
```

Implement the handler on the `*ShamHub` struct.

```go
func (sh *ShamHub) handleEditChange(ctx context.Context, req *editChangeRequest) (*editChangeResponse, error) {
    // ...

    // Return httpError for specific status codes.
    if !found {
        return nil, notFoundErrorf("change %s/%s#%d not found", owner, repo, num)
    }

    return &editChangeResponse{}, nil
}
```

Register the handler by adding the following line next to the definition:

```go
var _ = shamhubRESTHandler(pattern, (*ShamHub).handlerName)
```

For example:

```go
var _ = shamhubRESTHandler("PATCH /{owner}/{repo}/change/{number}", (*ShamHub).handleEditChange)
```

## Adding forge-level functionality

Forge is git-spice's abstraction for a code hosting forge.
shamhub's implementation of forge.Forge communicates with the server
using the REST API defined above.

To call the handler from the forge implementation,
add a method on `*forgeRepository`,
re-using the request/response structs defined above.

```go
func (r *forgeRepository) EditChange(...) (*forge.EditChangeResponse, error) {
    var req editChangeRequest
    // ...
    url := r.apiURL.JoinPath(...)
    var resp editChangeResponse
    if err := r.client.Patch(url, &req, &resp); err != nil {
        return nil, fmt.Errorf("failed to edit change: %w", err)
    }
}
```

## Adding command line functionality

Functionality for the shamhub CLI is defined in cli.go (for now).
Just add code there, and call directly into the `*ShamHub` implementation.
