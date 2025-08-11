package shamhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildRESTHandler_JSONBody(t *testing.T) {
	type request struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Message: req.Name + " has " + string(rune(req.Count+'0')) + " items"}, nil
	})

	body := `{"name":"test","count":5}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"message":"test has 5 items"}`, w.Body.String())
}

func TestBuildRESTHandler_PathParameters(t *testing.T) {
	type request struct {
		ID   string `json:"-" path:"id"`
		Name string `json:"name"`
	}
	type response struct {
		Result string `json:"result"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Result: "ID: " + req.ID + ", Name: " + req.Name}, nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /items/{id}", handler)

	body := `{"name":"example"}`
	req := httptest.NewRequest(http.MethodPost, "/items/123", strings.NewReader(body))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"result":"ID: 123, Name: example"}`, w.Body.String())
}

func TestBuildRESTHandler_FormParameters(t *testing.T) {
	type request struct {
		Filter   string `json:"-" form:"filter"`
		Required string `json:"-" form:"required,required"`
		Name     string `json:"name"`
	}
	type response struct {
		Data string `json:"data"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Data: req.Filter + ":" + req.Required + ":" + req.Name}, nil
	})

	body := `{"name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/test?filter=active&required=yes", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"data":"active:yes:test"}`, w.Body.String())
}

func TestBuildRESTHandler_IntegerPath(t *testing.T) {
	type request struct {
		ID    int    `json:"-" path:"id"`
		Count int64  `json:"-" path:"count"`
		Name  string `json:"name"`
	}
	type response struct {
		Sum int64 `json:"sum"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Sum: int64(req.ID) + req.Count}, nil
	})

	mux := http.NewServeMux()
	mux.Handle("POST /calc/{id}/{count}", handler)

	body := `{"name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/calc/42/58", strings.NewReader(body))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"sum":100}`, w.Body.String())
}

func TestBuildRESTHandler_GETRequest(t *testing.T) {
	type request struct {
		ID string `json:"-" path:"id"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Message: "Getting " + req.ID}, nil
	})

	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", handler)

	req := httptest.NewRequest(http.MethodGet, "/items/item123", nil)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"message":"Getting item123"}`, w.Body.String())
}

func TestBuildRESTHandler_MissingPathParameter(t *testing.T) {
	type request struct {
		ID string `json:"-" path:"id"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(struct{}, context.Context, request) (response, error) {
		return response{Message: "success"}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing required path parameter: id")
}

func TestBuildRESTHandler_MissingRequiredFormParameter(t *testing.T) {
	type request struct {
		Required string `json:"-" form:"required,required"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(struct{}, context.Context, request) (response, error) {
		return response{Message: "success"}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader("{}"))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing required form parameter: required")
}

func TestBuildRESTHandler_InvalidJSON(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(struct{}, context.Context, request) (response, error) {
		return response{Message: "success"}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"name":}`))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "decode request:")
}

func TestBuildRESTHandler_InvalidIntegerPath(t *testing.T) {
	type request struct {
		ID int `json:"-" path:"id"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(struct{}, context.Context, request) (response, error) {
		return response{Message: "success"}, nil
	})

	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", handler)

	req := httptest.NewRequest(http.MethodGet, "/items/not-a-number", nil)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "decode field id:")
}

func TestBuildRESTHandler_HTTPError(t *testing.T) {
	type request struct {
		ID string `json:"-" path:"id"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		if req.ID == "notfound" {
			return response{}, notFoundErrorf("item not found")
		}
		return response{Message: "success"}, nil
	})

	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", handler)

	req := httptest.NewRequest(http.MethodGet, "/items/notfound", nil)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "item not found")
}

func TestBuildRESTHandler_GenericError(t *testing.T) {
	type request struct {
		ID string `json:"-" path:"id"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(struct{}, context.Context, request) (response, error) {
		return response{}, assert.AnError
	})

	mux := http.NewServeMux()
	mux.Handle("GET /items/{id}", handler)

	req := httptest.NewRequest(http.MethodGet, "/items/test", nil)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "error:")
}

func TestBuildRESTHandler_PointerRequest(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req *request) (response, error) {
		return response{Message: "Hello " + req.Name}, nil
	})

	body := `{"name":"world"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"message":"Hello world"}`, w.Body.String())
}

func TestBuildRESTHandler_StateParameter(t *testing.T) {
	type state struct {
		Prefix string
	}
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Message string `json:"message"`
	}

	testState := state{Prefix: "Test: "}
	handler := buildRESTHandler(testState, func(s state, _ context.Context, req request) (response, error) {
		return response{Message: s.Prefix + req.Name}, nil
	})

	body := `{"name":"example"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"message":"Test: example"}`, w.Body.String())
}

func TestBuildRESTHandler_OptionalFormParameter(t *testing.T) {
	type request struct {
		Optional string `json:"-" form:"optional"`
		Name     string `json:"name"`
	}
	type response struct {
		Result string `json:"result"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		result := req.Name
		if req.Optional != "" {
			result += " (" + req.Optional + ")"
		}
		return response{Result: result}, nil
	})

	t.Run("WithOptionalParameter", func(t *testing.T) {
		body := `{"name":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/test?optional=value", strings.NewReader(body))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"result":"test (value)"}`, w.Body.String())
	})

	t.Run("WithoutOptionalParameter", func(t *testing.T) {
		body := `{"name":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.JSONEq(t, `{"result":"test"}`, w.Body.String())
	})
}

func TestBuildRESTHandler_UnknownJSONFields(t *testing.T) {
	type request struct {
		Name string `json:"name"`
	}
	type response struct {
		Message string `json:"message"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Message: req.Name}, nil
	})

	body := `{"name":"test","unknown":"field"}`
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "decode request:")
}

func TestBuildRESTHandler_QueryParameter(t *testing.T) {
	type request struct {
		Filter string `json:"-" query:"filter"`
		Name   string `json:"name"`
	}
	type response struct {
		Result string `json:"result"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Result: req.Name + " filtered by " + req.Filter}, nil
	})

	body := `{"name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/test?filter=active", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"result":"test filtered by active"}`, w.Body.String())
}

func TestBuildRESTHandler_IntegerFormParameter(t *testing.T) {
	type request struct {
		Limit int    `json:"-" form:"limit"`
		Name  string `json:"name"`
	}
	type response struct {
		Result string `json:"result"`
	}

	handler := buildRESTHandler(struct{}{}, func(_ struct{}, _ context.Context, req request) (response, error) {
		return response{Result: req.Name + " with limit " + string(rune(req.Limit+'0'))}, nil
	})

	body := `{"name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/test?limit=5", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"result":"test with limit 5"}`, w.Body.String())
}

func TestBuildRESTHandler_InvalidIntegerForm(t *testing.T) {
	type request struct {
		Limit int    `json:"-" form:"limit"`
		Name  string `json:"name"`
	}
	type response struct {
		Result string `json:"result"`
	}

	handler := buildRESTHandler(struct{}{}, func(struct{}, context.Context, request) (response, error) {
		return response{Result: "success"}, nil
	})

	body := `{"name":"test"}`
	req := httptest.NewRequest(http.MethodPost, "/test?limit=invalid", strings.NewReader(body))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "decode field limit:")
}
