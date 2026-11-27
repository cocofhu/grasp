package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"backend/internal/envutil"
	"golang.org/x/net/http/httpproxy"
)

// Codex omits the default tier on the wire. Apply the chat's tier at the
// Responses request boundary so an OpenAI project default cannot override it.
func startResponsesTierProxy(baseURL, modelID, tier string) (string, func(), error) {
	upstream, err := url.Parse(baseURL)
	if err != nil {
		return "", nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	config := httpproxy.FromEnvironment()
	allProxy := envutil.FirstNonEmpty("ALL_PROXY", "all_proxy")
	if config.HTTPProxy == "" {
		config.HTTPProxy = allProxy
	}
	if config.HTTPSProxy == "" {
		config.HTTPSProxy = allProxy
	}
	proxyForURL := config.ProxyFunc()
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		return proxyForURL(request.URL)
	}
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstream)
		},
	}
	tierValue, _ := json.Marshal(tier)
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && (r.URL.Path == "/responses" || r.URL.Path == "/responses/compact") {
				payload, err := io.ReadAll(r.Body)
				_ = r.Body.Close()
				var body map[string]json.RawMessage
				if err != nil || json.Unmarshal(payload, &body) != nil || body == nil {
					http.Error(w, "invalid Responses request body", http.StatusBadRequest)
					return
				}
				var model string
				// Standard covers all calls in the chat. Apply Fast only to
				// the selected model so auxiliary calls keep their own tier.
				if tier == "default" || (json.Unmarshal(body["model"], &model) == nil && model == modelID) {
					body["service_tier"] = tierValue
					payload, err = json.Marshal(body)
					if err != nil {
						http.Error(w, "could not encode Responses request body", http.StatusInternalServerError)
						return
					}
				}
				r.Body = io.NopCloser(bytes.NewReader(payload))
				r.ContentLength = int64(len(payload))
				r.Header.Del("Content-Length")
			}
			proxy.ServeHTTP(w, r)
		}),
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("codex: Responses tier proxy stopped: %v", err)
		}
	}()
	return "http://" + listener.Addr().String(), func() {
		_ = server.Close()
		transport.CloseIdleConnections()
	}, nil
}

func withLoopbackNoProxy(env []string) []string {
	const loopback = "127.0.0.1"
	// Keep the client's loopback hop local when a sandbox uses an HTTP proxy.
	for _, name := range []string{"NO_PROXY", "no_proxy"} {
		if value := envutil.Get(env, name); value != "" {
			env = envutil.Upsert(env, name, loopback+","+value)
		}
	}
	if envutil.Get(env, "NO_PROXY") == "" && envutil.Get(env, "no_proxy") == "" {
		env = envutil.Upsert(env, "NO_PROXY", loopback)
	}
	return env
}
