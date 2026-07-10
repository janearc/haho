package discovery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	serviceAccountCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	// insecureTLSEnv, when set to "1", allows the k3s discoverer to talk to the
	// Kubernetes API without verifying its certificate (dev only).
	insecureTLSEnv = "HAHO_K3S_INSECURE"
)

// Endpoint represents a discovered service endpoint.
type Endpoint struct {
	Host string
	Port int
}

// Addr returns the host:port string for this endpoint.
func (e *Endpoint) Addr() string { return fmt.Sprintf("%s:%d", e.Host, e.Port) }

// Discoverer finds service endpoints.
type Discoverer interface {
	Discover(ctx context.Context, serviceName, namespace string) (*Endpoint, error)
}

// --- StaticDiscoverer ---

// StaticDiscoverer returns a pre-configured endpoint. Useful for direct connections
// or development/testing.
type StaticDiscoverer struct {
	host string
	port int
}

// NewStaticDiscoverer creates a discoverer that always returns the given host:port.
func NewStaticDiscoverer(host string, port int) *StaticDiscoverer {
	return &StaticDiscoverer{host: host, port: port}
}

// Discover returns the static endpoint, ignoring serviceName and namespace.
func (d *StaticDiscoverer) Discover(ctx context.Context, serviceName, namespace string) (*Endpoint, error) {
	return &Endpoint{Host: d.host, Port: d.port}, nil
}

// --- K3sDiscoverer ---

// K3sDiscoverer discovers services via the Kubernetes API.
// It reads the in-cluster service account token and CA cert.
// This is a seam: the actual k8s client-go dependency can be swapped in later.
// For now, it uses the raw Kubernetes REST API via net/http.
type K3sDiscoverer struct {
	apiHost string
	token   string
	client  *http.Client
}

// k8s Endpoints response types (unexported).
type k8sEndpoints struct {
	Subsets []k8sSubset `json:"subsets"`
}

type k8sSubset struct {
	Addresses []k8sAddress `json:"addresses"`
	Ports     []k8sPort    `json:"ports"`
}

type k8sAddress struct {
	IP string `json:"ip"`
}

type k8sPort struct {
	Port int `json:"port"`
}

// NewK3sDiscoverer creates a discoverer that uses the k8s API.
// If apiHost is empty, reads KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT env vars.
// If token is empty, reads /var/run/secrets/kubernetes.io/serviceaccount/token.
// The CA cert is read from /var/run/secrets/kubernetes.io/serviceaccount/ca.crt if available.
func NewK3sDiscoverer(apiHost, token string) (*K3sDiscoverer, error) {
	if apiHost == "" {
		host := os.Getenv("KUBERNETES_SERVICE_HOST")
		port := os.Getenv("KUBERNETES_SERVICE_PORT")
		if host == "" || port == "" {
			return nil, fmt.Errorf("KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be set")
		}
		apiHost = host + ":" + port
	}

	if token == "" {
		tokenBytes, err := os.ReadFile(serviceAccountTokenPath)
		if err != nil {
			return nil, fmt.Errorf("reading service account token: %w", err)
		}
		token = string(tokenBytes)
	}

	// Build TLS config from the in-cluster CA. If the CA cert is unavailable,
	// refuse to talk to the API server unverified unless the operator has
	// explicitly opted in — a silent InsecureSkipVerify is a MITM window on a
	// hostile network.
	tlsConfig := &tls.Config{}
	caCert, err := os.ReadFile(serviceAccountCAPath)
	if err == nil {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("service account CA cert at %s is not valid PEM", serviceAccountCAPath)
		}
		tlsConfig.RootCAs = pool
	} else if os.Getenv(insecureTLSEnv) == "1" {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // explicit opt-in
	} else {
		return nil, fmt.Errorf("reading service account CA cert: %w (set %s=1 to skip TLS verification for dev)", err, insecureTLSEnv)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return &K3sDiscoverer{
		apiHost: apiHost,
		token:   token,
		client:  client,
	}, nil
}

// Discover queries the Kubernetes API for the named service's endpoints.
func (d *K3sDiscoverer) Discover(ctx context.Context, serviceName, namespace string) (*Endpoint, error) {
	url := fmt.Sprintf("https://%s/api/v1/namespaces/%s/endpoints/%s", d.apiHost, namespace, serviceName)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.token)
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting endpoints: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("k8s API returned %d: %s", resp.StatusCode, string(body))
	}

	var endpoints k8sEndpoints
	if err := json.NewDecoder(resp.Body).Decode(&endpoints); err != nil {
		return nil, fmt.Errorf("decoding endpoints response: %w", err)
	}

	for _, subset := range endpoints.Subsets {
		if len(subset.Addresses) > 0 && len(subset.Ports) > 0 {
			return &Endpoint{
				Host: subset.Addresses[0].IP,
				Port: subset.Ports[0].Port,
			}, nil
		}
	}

	return nil, fmt.Errorf("no ready endpoints found for %s/%s", namespace, serviceName)
}

// --- Factory function ---

// New creates a Discoverer based on the discovery method string.
// Supported: "k3s" (kubernetes), "static" (direct).
// For "static", host and port must be provided.
func New(method, host string, port int) (Discoverer, error) {
	switch method {
	case "k3s":
		return NewK3sDiscoverer("", "")
	case "static":
		return NewStaticDiscoverer(host, port), nil
	default:
		return nil, fmt.Errorf("unknown discovery method: %q", method)
	}
}
