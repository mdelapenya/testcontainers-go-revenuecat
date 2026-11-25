package revenuecat

import (
	"errors"
	"net/url"

	"github.com/testcontainers/testcontainers-go"
)

type options struct {
	specURL string
}

// Option configures the RevenueCat module.
type Option func(*options) error

var _ testcontainers.ContainerCustomizer = (Option)(nil)

// Customize implements testcontainers.ContainerCustomizer. Run applies the
// module option before constructing the container request.
func (o Option) Customize(*testcontainers.GenericContainerRequest) error { return nil }

// WithSpecURL replaces the embedded fixture with a Microcks-compatible
// OpenAPI 3 document fetched from an HTTP or HTTPS URL.
func WithSpecURL(specURL string) Option {
	return func(o *options) error {
		parsed, err := url.Parse(specURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
			return errors.New("expected an absolute HTTP(S) spec URL")
		}
		o.specURL = specURL
		return nil
	}
}
