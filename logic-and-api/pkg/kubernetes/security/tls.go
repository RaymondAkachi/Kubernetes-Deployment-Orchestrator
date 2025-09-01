package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/RaymondAkachi/Kubernetes-Deployment-Orchestrator/logic-and-api/pkg/config"
)

// LoadAndVerifyTLSConfig loads a TLS configuration and performs comprehensive checks on the certificate's validity.
func LoadAndVerifyTLSConfig(cfg *config.TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// 1. Load the key pair from files.
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS key pair: %w", err)
	}

	// 2. Manually parse the leaf certificate to perform deeper checks.
	// The first certificate in the chain is the leaf certificate.
	leafCert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	// 3. **CRITICAL: Check for certificate expiration.**
	now := time.Now()
	if now.After(leafCert.NotAfter) {
		return nil, fmt.Errorf("TLS certificate has expired on %v", leafCert.NotAfter)
	}
	
    // Proactive check: Warn if the certificate is expiring soon (e.g., within 30 days).
	// This helps catch issues before they become outages.
	expirationThreshold := 30 * 24 * time.Hour 
	if now.Add(expirationThreshold).After(leafCert.NotAfter) {
		// This could be a log warning instead of a hard error, depending on your policy.
		fmt.Printf("Warning: TLS certificate is nearing expiration. It will expire on %v\n", leafCert.NotAfter)
	}

	// 4. **CRITICAL: Verify the certificate's intended key usage.**
	// Ensure the certificate is designated for server authentication.
	isServerAuth := false
	for _, usage := range leafCert.ExtKeyUsage {
		if usage == x509.ExtKeyUsageServerAuth {
			isServerAuth = true
			break
		}
	}
	if !isServerAuth {
		return nil, fmt.Errorf("TLS certificate is not valid for server authentication")
	}

	// 5. Load the CA pool for client certificate verification.
	caCertPool := x509.NewCertPool()
	if cfg.CAFile != "" {
		caCertBytes, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file: %w", err)
		}
		if ok := caCertPool.AppendCertsFromPEM(caCertBytes); !ok {
			return nil, fmt.Errorf("failed to append CA certificates to pool from %s", cfg.CAFile)
		}
	}

	// 6. Return the secure TLS configuration.
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    caCertPool,
		ClientAuth:   tls.RequireAndVerifyClientCert, // Enforce mTLS
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256, // Corrected: POLY1305_SHA256
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}, nil
}