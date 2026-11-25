# Testcontainers for Go module for RevenueCat

Run a local mock of the [RevenueCat REST API v2](https://www.revenuecat.com/docs/api-v2) for integration tests. The module imports an embedded OpenAPI fixture into [Microcks](https://microcks.io/) and exposes the mock through a Testcontainers container.

## Install

```sh
go get github.com/mdelapenya/testcontainers-go-revenuecat
```

Requires Go 1.26 or later and a Docker-compatible container runtime.

## Use in a test

```go
package example_test

import (
	"net/http"
	"testing"

	revenuecat "github.com/mdelapenya/testcontainers-go-revenuecat"
	"github.com/testcontainers/testcontainers-go"
)

func TestRevenueCat(t *testing.T) {
	ctx := t.Context()
	ctr, err := revenuecat.Run(ctx, revenuecat.DefaultImage)
	testcontainers.CleanupContainer(t, ctr)
	if err != nil {
		t.Fatal(err)
	}

	baseURL, err := ctr.BaseURL(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Configure your RevenueCat client with baseURL, or call it directly.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/projects", nil)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list projects: %s", resp.Status)
	}
}
```

`BaseURL` takes the place of `https://api.revenuecat.com/v2`. Append operation paths such as `/projects/{project_id}/customers` directly. `Run` accepts standard Testcontainers options for networking, environment variables, and other container configuration.

## Default responses

The embedded fixture covers 144 operations from the RevenueCat API v2 reference, including projects, customers, subscriptions, entitlements, purchases, and virtual currencies. Each operation serves a fixed example for its documented success or redirect status. Path IDs and query parameters can vary. JSON response values are fixtures, write operations have no persistent effects, and list fixtures have a single page. Use a dummy key if your client requires one. The mock serves responses without authenticating clients or enforcing rate limits.

## Custom OpenAPI fixture

Pass `WithSpecURL` to test scenarios such as API errors or different payloads. The URL must serve a self-contained OpenAPI 3 document in YAML or JSON. Define named response examples and dispatch rules using the [Microcks OpenAPI conventions](https://microcks.io/documentation/references/artifacts/openapi-conventions/). The document's `info.title` and `info.version` become the service name and version. `Run` returns an error if the custom fixture cannot be fetched or imported.

```go
ctr, err := revenuecat.Run(ctx, revenuecat.DefaultImage,
	revenuecat.WithSpecURL("https://example.test/revenuecat-openapi.yaml"),
)
```

## Refresh the API snapshot for a release

```sh
go generate ./...
go test -count=1 ./...
```

The generator reads the current RevenueCat REST API v2 resource definitions and writes `testdata/revenuecat.yaml`. Commit any fixture changes before tagging a module release. Each Go module version embeds the snapshot from its own tag, so default startup only requires the Microcks image. `ServiceVersion()` reports the API version declared in the snapshot.
