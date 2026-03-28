package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var (
	// _blockedPatterns contains URL patterns that should not be scraped.
	_blockedPatterns = []*regexp.Regexp{
		// Admin consoles
		regexp.MustCompile(`(?i)/phpmyadmin`),
		regexp.MustCompile(`(?i)/adminer`),
		regexp.MustCompile(`(?i)/wp-admin`),
		regexp.MustCompile(`(?i)/wp-login`),

		// Debug/internal endpoints
		regexp.MustCompile(`(?i)/actuator`),
		regexp.MustCompile(`(?i)/debug/pprof`),
		regexp.MustCompile(`(?i)/healthz`),
		regexp.MustCompile(`(?i)/\.env`),
		regexp.MustCompile(`(?i)/server-status`),

		// Database ports in URL
		regexp.MustCompile(`:(27017|6379|5432|3306|9200|9300|11211)\b`),
	}

	// _privateNetworks contains CIDR ranges that should be blocked (SSRF protection).
	_privateNetworks = []string{
		"127.0.0.0/8",    // Loopback
		"10.0.0.0/8",     // Private Class A
		"172.16.0.0/12",  // Private Class B
		"192.168.0.0/16", // Private Class C
		"169.254.0.0/16", // Link-local
		"0.0.0.0/8",      // Current network
		"::1/128",          // IPv6 loopback
		"fc00::/7",         // IPv6 unique local
		"fe80::/10",        // IPv6 link-local
		"fd00:ec2::254/128", // AWS EC2 metadata (IPv6)
		"100.100.100.200/32", // Alibaba Cloud metadata
	}

	// _parsedNetworks is the parsed form of _privateNetworks.
	_parsedNetworks = func() []*net.IPNet {
		var nets []*net.IPNet
		for _, cidr := range _privateNetworks {
			_, network, err := net.ParseCIDR(cidr)
			if err != nil {
				panic("invalid CIDR in _privateNetworks: " + cidr)
			}
			nets = append(nets, network)
		}
		return nets
	}()

	_defaultValidator = NewValidator(net.DefaultResolver)
)

// IPResolver resolves hostnames to IP addresses.
type IPResolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// Validator validates URLs against the SSRF policy.
type Validator struct {
	resolver IPResolver
}

// BlockedURLError indicates a URL was rejected by the SSRF policy.
type BlockedURLError struct {
	URL string
	Err error
}

func (e *BlockedURLError) Error() string {
	if e == nil || e.Err == nil {
		return "URL is not allowed"
	}
	return e.Err.Error()
}

func (e *BlockedURLError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewValidator creates a URL validator with the provided resolver.
func NewValidator(resolver IPResolver) *Validator {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Validator{resolver: resolver}
}

// DefaultValidator returns the process-wide validator used in production code.
func DefaultValidator() *Validator {
	return _defaultValidator
}

// IsBlockedError reports whether err represents a policy-blocked URL.
func IsBlockedError(err error) bool {
	var blockedErr *BlockedURLError
	return errors.As(err, &blockedErr)
}

// ValidateURL checks if the URL is safe to fetch.
// Returns an error if the URL targets a private IP or matches a blocked pattern.
func ValidateURL(rawURL string) error {
	return _defaultValidator.ValidateURL(context.Background(), rawURL)
}

// ValidateURL checks if the URL is safe to fetch using the configured resolver.
// Returns an error if the URL targets a private IP or matches a blocked pattern.
func (v *Validator) ValidateURL(ctx context.Context, rawURL string) error {
	if ctx == nil {
		ctx = context.Background()
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return fmt.Errorf("invalid URL: missing hostname")
	}

	// Check URL against blocked patterns
	for _, pattern := range _blockedPatterns {
		if pattern.MatchString(rawURL) {
			return &BlockedURLError{
				URL: rawURL,
				Err: fmt.Errorf("URL matches blocked pattern: %s", pattern.String()),
			}
		}
	}

	// Block cloud metadata endpoints
	switch strings.ToLower(hostname) {
	case "metadata.google.internal", // GCP
		"metadata.azure.com",       // Azure
		"management.azure.com":     // Azure management
		return &BlockedURLError{
			URL: rawURL,
			Err: fmt.Errorf("access to cloud metadata endpoints is not allowed"),
		}
	}

	// IP literals do not need DNS resolution.
	if ip := net.ParseIP(hostname); ip != nil {
		if isPrivateIP(ip) {
			return &BlockedURLError{
				URL: rawURL,
				Err: fmt.Errorf("access to private/internal addresses is not allowed (%s)", ip.String()),
			}
		}
		return nil
	}

	// Resolve hostname to IP and check against private ranges
	ips, err := v.resolver.LookupIP(ctx, "ip", hostname)
	if err != nil {
		return fmt.Errorf("could not resolve hostname %q: %w", hostname, err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return &BlockedURLError{
				URL: rawURL,
				Err: fmt.Errorf("access to private/internal addresses is not allowed (%s resolves to %s)", hostname, ip.String()),
			}
		}
	}

	return nil
}

// isPrivateIP checks if an IP falls within any private/reserved range.
func isPrivateIP(ip net.IP) bool {
	for _, network := range _parsedNetworks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
