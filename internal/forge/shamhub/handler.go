package shamhub

import "net/http"

type shamhubEndpoint struct {
	Pattern string
	Handler func(*ShamHub, http.ResponseWriter, *http.Request)
}

var _handlers []shamhubEndpoint

func shamhubHandler(pattern string, handler func(*ShamHub, http.ResponseWriter, *http.Request)) struct{} {
	_handlers = append(_handlers, shamhubEndpoint{Pattern: pattern, Handler: handler})
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
