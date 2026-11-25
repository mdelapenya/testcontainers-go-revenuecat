// Package revenuecat provides a containerized mock of the RevenueCat REST API v2.
package revenuecat

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"gopkg.in/yaml.v3"
	microcks "microcks.io/testcontainers-go"
)

// DefaultImage is the default Microcks image used to serve the mock.
const DefaultImage = "quay.io/microcks/microcks-uber:1.16.0"

//go:generate go run ./internal/cmd/updatespec
//go:embed testdata/revenuecat.yaml
var embeddedSpec []byte

// Container is a Microcks container preloaded with a RevenueCat OpenAPI fixture.
type Container struct {
	*microcks.MicrocksContainer
	serviceName    string
	serviceVersion string
}

// Run starts Microcks and imports the embedded RevenueCat fixture or the fixture
// supplied by WithSpecURL. Register cleanup even if Run returns an error.
func Run(ctx context.Context, img string, opts ...testcontainers.ContainerCustomizer) (*Container, error) {
	var config options
	var containerOpts []testcontainers.ContainerCustomizer
	for _, opt := range opts {
		if revenuecatOpt, ok := opt.(Option); ok {
			if err := revenuecatOpt(&config); err != nil {
				return nil, fmt.Errorf("configure revenuecat: %w", err)
			}
		} else {
			containerOpts = append(containerOpts, opt)
		}
	}

	spec, err := loadSpec(ctx, config.specURL)
	if err != nil {
		return nil, fmt.Errorf("load revenuecat spec: %w", err)
	}

	defaults := []testcontainers.ContainerCustomizer{
		testcontainers.WithExposedPorts(microcks.DefaultHttpPort, microcks.DefaultGrpcPort),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/api/health").WithPort(microcks.DefaultHttpPort)),
	}
	containerOpts = append(defaults, containerOpts...)
	containerOpts = append(containerOpts, testcontainers.CustomizeRequestOption(func(req *testcontainers.GenericContainerRequest) error {
		// Import the main fixture before caller-supplied post-ready hooks.
		hooks := testcontainers.ContainerLifecycleHooks{
			PostReadies: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					return importSpec(ctx, &microcks.MicrocksContainer{Container: c}, spec.data)
				},
			},
		}
		req.LifecycleHooks = append([]testcontainers.ContainerLifecycleHooks{hooks}, req.LifecycleHooks...)
		return nil
	}))

	dockerContainer, err := testcontainers.Run(ctx, img, containerOpts...)
	var c *Container
	if dockerContainer != nil {
		c = &Container{
			MicrocksContainer: &microcks.MicrocksContainer{Container: dockerContainer},
			serviceName:       spec.Info.Title,
			serviceVersion:    spec.Info.Version,
		}
	}
	if err != nil {
		return c, fmt.Errorf("run revenuecat: %w", err)
	}
	return c, nil
}

func importSpec(ctx context.Context, c *microcks.MicrocksContainer, data []byte) error {
	dir, err := os.MkdirTemp("", "revenuecat-openapi-*")
	if err != nil {
		return fmt.Errorf("create spec directory: %w", err)
	}
	defer os.RemoveAll(dir)

	path := filepath.Join(dir, "revenuecat.yaml")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write revenuecat spec: %w", err)
	}
	status, err := c.ImportAsMainArtifact(ctx, path)
	if err != nil {
		return fmt.Errorf("import revenuecat spec: %w", err)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("import revenuecat spec: HTTP %d", status)
	}
	return nil
}

// ServiceName returns the title of the imported OpenAPI fixture.
func (c *Container) ServiceName() string { return c.serviceName }

// ServiceVersion returns the version of the imported OpenAPI fixture.
func (c *Container) ServiceVersion() string { return c.serviceVersion }

// BaseURL returns the mock endpoint for this service. Append operation paths
// such as /projects directly to this URL.
func (c *Container) BaseURL(ctx context.Context) (string, error) {
	return c.RestMockEndpoint(ctx, url.PathEscape(c.serviceName), url.PathEscape(c.serviceVersion))
}

type openAPISpec struct {
	data    []byte
	OpenAPI string
	Info    struct {
		Title   string
		Version string
	}
	Paths map[string]any
}

func loadSpec(ctx context.Context, specURL string) (openAPISpec, error) {
	data := embeddedSpec
	if specURL != "" {
		var err error
		data, err = downloadSpec(ctx, specURL)
		if err != nil {
			return openAPISpec{}, err
		}
	}
	spec := openAPISpec{data: data}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return openAPISpec{}, fmt.Errorf("parse OpenAPI: %w", err)
	}
	if !strings.HasPrefix(spec.OpenAPI, "3.") || len(spec.Paths) == 0 {
		return openAPISpec{}, errors.New("expected an OpenAPI 3 document with paths")
	}
	if !validPathSegment(spec.Info.Title) || !validPathSegment(spec.Info.Version) {
		return openAPISpec{}, errors.New("info.title and info.version must be usable as URL path segments")
	}
	return spec, nil
}

func validPathSegment(value string) bool {
	return strings.TrimSpace(value) != "" && value != "." && value != ".." &&
		!strings.ContainsAny(value, "/\\")
}

func downloadSpec(ctx context.Context, specURL string) ([]byte, error) {
	const maxSpecSize = 16 << 20

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, specURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create spec request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download spec: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download spec: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSpecSize+1))
	if err != nil {
		return nil, fmt.Errorf("read spec: %w", err)
	}
	if len(data) > maxSpecSize {
		return nil, fmt.Errorf("spec exceeds %d bytes", maxSpecSize)
	}
	return data, nil
}
