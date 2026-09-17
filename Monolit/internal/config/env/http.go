package env

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type httpEnvConfig struct {
	Host        string        `env:"HTTP_HOST,required"`
	Port        string        `env:"HTTP_PORT,required"`
	ReadTimeout time.Duration `env:"HTTP_READ_TIMEOUT,required"`
	// TrustedProxyCIDRs lists the networks a forwarded client address may be
	// believed from. Empty means the API is reached directly and no forwarded
	// header is trusted at all.
	TrustedProxyCIDRs []string `env:"TRUSTED_PROXY_CIDRS" envSeparator:","`
}

type httpConfig struct {
	raw            httpEnvConfig
	trustedProxies []*net.IPNet
}

func NewHTTPConfig() (*httpConfig, error) {
	var raw httpEnvConfig
	if err := env.Parse(&raw); err != nil {
		return nil, err
	}

	config := &httpConfig{raw: raw}
	for _, entry := range raw.TrustedProxyCIDRs {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// A bare address is the same as a single-host network, which is what a
		// deployment usually means by "our proxy".
		if !strings.Contains(entry, "/") {
			if ip := net.ParseIP(entry); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				entry = fmt.Sprintf("%s/%d", entry, bits)
			}
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q: %w", entry, err)
		}
		config.trustedProxies = append(config.trustedProxies, network)
	}

	return config, nil
}

// IsTrustedProxy says whether an address belongs to the deployment's own front
// layer. Only such an address may speak for somebody else.
func (config *httpConfig) IsTrustedProxy(address string) bool {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil {
		return false
	}
	for _, network := range config.trustedProxies {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}

func (config *httpConfig) Address() string {
	return net.JoinHostPort(config.raw.Host, config.raw.Port)
}

func (config *httpConfig) ReadTimeout() time.Duration {
	return config.raw.ReadTimeout
}
