package revenuecat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"gopkg.in/yaml.v3"
)

func TestEmbeddedAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	ctr, err := Run(ctx, DefaultImage, testcontainers.WithLifecycleHooks(testcontainers.ContainerLifecycleHooks{}))
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	baseURL, err := ctr.BaseURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ctr.ServiceName() != "RevenueCat" || ctr.ServiceVersion() == "" {
		t.Fatalf("unexpected service %q version %q", ctr.ServiceName(), ctr.ServiceVersion())
	}

	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name string
				In   string
			}
			Responses map[string]struct {
				Content map[string]struct {
					Examples map[string]struct {
						Value any
					}
				}
				Headers map[string]struct {
					Examples map[string]struct {
						Value any
					}
				}
			}
		}
	}
	if err := yaml.Unmarshal(embeddedSpec, &spec); err != nil {
		t.Fatal(err)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	for path, methods := range spec.Paths {
		for method, operation := range methods {
			t.Run(strings.ToUpper(method)+" "+path, func(t *testing.T) {
				address := baseURL + path
				for _, param := range operation.Parameters {
					if param.In == "path" {
						address = strings.ReplaceAll(address, "{"+param.Name+"}", url.PathEscape("test-"+param.Name))
					}
				}
				// A query string should leave the response selection unchanged.
				address += "?limit=3"
				for code, response := range operation.Responses {
					status, err := strconv.Atoi(code)
					if err != nil {
						t.Fatal(err)
					}
					if len(response.Content) == 0 {
						result, data := call(t, client, method, address, "")
						if result.StatusCode != status || len(data) != 0 {
							t.Fatalf("got %d %q, want %d with an empty body", result.StatusCode, data, status)
						}
						for name, header := range response.Headers {
							want := fmt.Sprint(header.Examples["success"].Value)
							if got := result.Header.Get(name); got != want {
								t.Errorf("header %s got %q, want %q", name, got, want)
							}
						}
					}
					for mediaType, content := range response.Content {
						result, data := call(t, client, method, address, mediaType)
						if result.StatusCode != status {
							t.Fatalf("got %d %s, want %d", result.StatusCode, data, status)
						}
						want, err := json.Marshal(content.Examples["success"].Value)
						if err != nil {
							t.Fatal(err)
						}
						if mediaType == "application/json" {
							assertJSON(t, data, want)
						}
					}
				}
			})
		}
	}
}

func TestWithSpecURL(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	testcontainers.SkipIfProviderIsNotHealthy(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `openapi: 3.0.3
info:
  title: RevenueCat Custom
  version: 2.1.0
paths:
  /projects:
    get:
      responses:
        "200":
          description: OK
          content:
            application/json:
              examples:
                custom:
                  value:
                    object: list
                    items: []
`)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	ctr, err := Run(ctx, DefaultImage, WithSpecURL(server.URL))
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}
	if ctr.ServiceName() != "RevenueCat Custom" || ctr.ServiceVersion() != "2.1.0" {
		t.Fatalf("unexpected service %q version %q", ctr.ServiceName(), ctr.ServiceVersion())
	}
	baseURL, err := ctr.BaseURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	response, data := call(t, &http.Client{Timeout: 10 * time.Second}, "get", baseURL+"/projects", "application/json")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("custom fixture returned %d %s", response.StatusCode, data)
	}
	assertJSON(t, data, []byte(`{"object":"list","items":[]}`))
}

func TestSpecDownloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	ctr, err := Run(t.Context(), DefaultImage, WithSpecURL(server.URL))
	if ctr != nil || err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("expected a spec download error before starting Docker, got %v, %v", ctr, err)
	}
}

func call(t *testing.T, client *http.Client, method, address, accept string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), strings.ToUpper(method), address, nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func assertJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode response %q: %v", got, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Errorf("got %s, want %s", got, want)
	}
}
