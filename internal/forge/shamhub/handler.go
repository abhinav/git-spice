package shamhub

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

type shamhubEndpoint struct {
	Pattern string
	Handler func(*ShamHub, http.ResponseWriter, *http.Request)
}

var _handlers []shamhubEndpoint

// buildRESTHandler creates a generic HTTP handler that processes JSON-based requests.
// Handlers may return httpError to specify HTTP status codes.
//
// Request fields may be tagged with:
//
//   - `json:"name"` to extract values from the JSON body.
//   - `path:"name"` to extract values from the URL path.
//     These fields are always required.
//   - `form:"name"` or `query:"name"` to extract values from the form data.
//     These fields are optional unless the tag is `form:"name,required"`.
//
// Fields can only be tagged with one of `path` and `form`,
// and they MUST be tagged with `json:"-"`.
// They must be one of the following types:
//
//   - string
//   - integer types
func buildRESTHandler[State, Req, Res any](state State, handler func(State, context.Context, Req) (Res, error)) http.Handler {
	reqType := reflect.TypeFor[Req]()
	reqIsPointer := reqType.Kind() == reflect.Pointer
	if reqIsPointer {
		reqType = reqType.Elem()
	}
	if reqType.Kind() != reflect.Struct {
		panic(fmt.Sprintf("shamhubJSONHandler: expected a struct type for request, got %v (%v)", reqType, reqType.Kind()))
	}

	type formField struct {
		Required bool // whether this field is required
		Index    int  // index in the request struct
	}

	pathFields := make(map[string]int)                            // field to index
	formFields := make(map[string]formField)                      // field to index
	decoders := make(map[int]func(string) (reflect.Value, error)) // field index to decoder
	for idx := range reqType.NumField() {
		field := reqType.Field(idx)

		pathTag := field.Tag.Get("path")
		formTag := cmp.Or(field.Tag.Get("form"), field.Tag.Get("query"))
		jsonTag := field.Tag.Get("json")

		if pathTag != "" && formTag != "" {
			panic(fmt.Sprintf(`%v.%s cannot have both path and form tags`, reqType, field.Name))
		}
		if pathTag == "" && formTag == "" && jsonTag == "" {
			panic(fmt.Sprintf(`%v.%s must have at least one of path, form, or json tags`, reqType, field.Name))
		}
		if (pathTag != "" || formTag != "") && jsonTag != "-" {
			panic(fmt.Sprintf(`%v.%s must have json:"-" tag to avoid serialization`, reqType, field.Name))
		}

		if jsonTag != "-" {
			continue
		}

		switch field.Type.Kind() {
		case reflect.String:
			// works as is
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			decoders[idx] = func(value string) (reflect.Value, error) {
				n, err := strconv.ParseInt(value, 10, field.Type.Bits())
				if err != nil {
					return reflect.Value{}, fmt.Errorf("parse int for field %s: %w", field.Name, err)
				}
				return reflect.ValueOf(n).Convert(field.Type), nil
			}

		default:
			panic(fmt.Sprintf(`%v.%s with path tag must be a string`, reqType, field.Name))
		}

		switch {
		case pathTag != "":
			pathFields[pathTag] = idx

		case formTag != "":
			name, optsStr, _ := strings.Cut(formTag, ",")
			opts := strings.Split(optsStr, ",")
			if name == "" {
				panic(fmt.Sprintf(`%v.%s with form tag must have a name`, reqType, field.Name))
			}

			formFields[name] = formField{
				Required: slices.Contains(opts, "required"),
				Index:    idx,
			}

		default:
			panic(fmt.Sprintf(`%v.%s must have either path or form tag`, reqType, field.Name))
		}

	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fields requested by "form" or "path".
		// Will be decoded later.
		nonJSONFields := make(map[int]string)     // field index to string value
		nonJSONFieldNames := make(map[int]string) // field index to name
		for name, idx := range pathFields {
			value := r.PathValue(name)
			if value == "" {
				http.Error(w, "missing required path parameter: "+name, http.StatusBadRequest)
				return
			}
			nonJSONFields[idx] = value
			nonJSONFieldNames[idx] = name
		}
		for name, field := range formFields {
			value := r.FormValue(name)
			if value == "" && field.Required {
				http.Error(w, "missing required form parameter: "+name, http.StatusBadRequest)
				return
			}
			if value != "" {
				nonJSONFields[field.Index] = value
				nonJSONFieldNames[field.Index] = name
			}
		}

		reqv := reflect.New(reqType).Elem()
		for idx, value := range nonJSONFields {
			if dec, ok := decoders[idx]; ok {
				value, err := dec(value)
				if err != nil {
					name := nonJSONFieldNames[idx]
					http.Error(w, fmt.Sprintf("decode field %s: %v", name, err), http.StatusBadRequest)
					return
				}
				reqv.Field(idx).Set(value)
			} else {
				reqv.Field(idx).SetString(value)
			}
		}

		// Decode the body only if it's not a GET request.
		if r.Method != http.MethodGet {
			dec := json.NewDecoder(r.Body)
			dec.DisallowUnknownFields()
			if err := dec.Decode(reqv.Addr().Interface()); err != nil {
				http.Error(w, fmt.Sprintf("decode request: %v", err), http.StatusBadRequest)
				return
			}
		}

		var req any
		if reqIsPointer {
			req = reqv.Addr().Interface()
		} else {
			req = reqv.Interface()
		}

		res, err := handler(state, r.Context(), req.(Req))
		if err != nil {
			var httpErr *httpError
			if errors.As(err, &httpErr) {
				http.Error(w, httpErr.Error(), httpErr.code)
			} else {
				http.Error(w, fmt.Sprintf("error: %v", err), http.StatusInternalServerError)
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ") // pretty print JSON
		if err := enc.Encode(res); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}

// shamhubRESTHandler allows conveniently defining JSON-based handlers for ShamHub.
// Handlers may return httpError to specify HTTP status codes.
//
// Request fields may be tagged with:
//
//   - `json:"name"` to extract values from the JSON body.
//   - `path:"name"` to extract values from the URL path.
//     These fields are always required.
//   - `form:"name"` or `query:"name"` to extract values from the form data.
//     These fields are optional unless the tag is `form:"name,required"`.
//
// Fields can only be tagged with one of `path` and `form`,
// and they MUST be tagged with `json:"-"`.
// They must be one of the following types:
//
//   - string
//   - integer types
func shamhubRESTHandler[Req, Res any](pattern string, handler func(*ShamHub, context.Context, Req) (Res, error)) struct{} {
	_handlers = append(_handlers, shamhubEndpoint{
		Pattern: pattern,
		Handler: func(sh *ShamHub, w http.ResponseWriter, r *http.Request) {
			restHandler := buildRESTHandler(sh, func(state *ShamHub, ctx context.Context, req Req) (Res, error) {
				return handler(state, ctx, req)
			})
			restHandler.ServeHTTP(w, r)
		},
	})
	return struct{}{} // no-op return to use this without func init()
}

func (sh *ShamHub) apiHandler() http.Handler {
	mux := http.NewServeMux()

	for _, ep := range _handlers {
		mux.HandleFunc(ep.Pattern, func(w http.ResponseWriter, r *http.Request) {
			ep.Handler(sh, w, r)
		})
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sh.log.Errorf("Unexpected request: %s %s", r.Method, r.URL.Path)
		http.Error(w, "not found", http.StatusNotFound)
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sh.log.Infof("ShamHub: %s %s", r.Method, r.URL.String())

		// Everything except /auth/login requires a token.
		if r.URL.Path != "/login" {
			token := r.Header.Get("Authentication-Token")
			if token == "" {
				http.Error(w, "missing token", http.StatusUnauthorized)
				return
			}

			sh.mu.RLock()
			_, ok := sh.tokens[token]
			sh.mu.RUnlock()
			if !ok {
				http.Error(w, "invalid token", http.StatusUnauthorized)
				return
			}
		}

		mux.ServeHTTP(w, r)
	})
}

// httpError allows handlers to return specific HTTP status codes
type httpError struct {
	code    int
	message string
}

func (e *httpError) Error() string {
	return e.message
}

func notFoundErrorf(msg string, args ...any) error {
	return &httpError{code: http.StatusNotFound, message: fmt.Sprintf(msg, args...)}
}

func badRequestErrorf(msg string, args ...any) error {
	return &httpError{code: http.StatusBadRequest, message: fmt.Sprintf(msg, args...)}
}
