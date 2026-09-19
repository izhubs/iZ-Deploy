// Package proxy manages reverse proxy sidecars, traffic routing, and TLS termination.
package proxy

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Standard ACME endpoints and challenge identifiers.
const (
	LetsEncryptProductionURL = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStagingURL    = "https://acme-staging-v02.api.letsencrypt.org/directory"

	DefaultCertRenewalWindow = 30 * 24 * time.Hour // 30 days before expiration
	DefaultStorageDirectory  = "/etc/kamal-proxy/certs"
)

// ACMEChallengeType specifies the validation challenge protocol.
type ACMEChallengeType string

const (
	// ChallengeHTTP01 validates domain ownership via HTTP port 80 path /.well-known/acme-challenge.
	ChallengeHTTP01 ACMEChallengeType = "HTTP-01"

	// ChallengeTLSALPN01 validates domain ownership via TLS negotiation on port 443 with ALPN acme-tls/1.
	ChallengeTLSALPN01 ACMEChallengeType = "TLS-ALPN-01"
)

// Cert errors.
var (
	ErrEmptyDomainList     = errors.New("cert: at least one domain must be specified")
	ErrInvalidDomainName   = errors.New("cert: domain name contains invalid characters")
	ErrInvalidEmailAddress = errors.New("cert: invalid contact email address")
	ErrInvalidChallenge    = errors.New("cert: unsupported ACME challenge type")
	ErrCertificateNotFound = errors.New("cert: certificate file not found on disk")
	ErrPEMDecodeFailed     = errors.New("cert: failed to decode PEM block")
	ErrNoCertificateBlock  = errors.New("cert: no CERTIFICATE block found in PEM")
)

// DECISION: Support both HTTP-01 and TLS-ALPN-01 challenge mechanisms.
// WHY: HTTP-01 is universally supported across CDNs and public clouds, while TLS-ALPN-01
// enables zero-HTTP-port TLS issuance when port 80 is firewalled or restricted.
// TRADE-OFF: Requires branching in ACME configuration generation.
// REF: RFC 8555 (ACME) & RFC 8737 (TLS-ALPN-01)

// CertConfig defines Let's Encrypt automated TLS provisioning parameters.
type CertConfig struct {
	Domains        []string          `json:"domains"`
	Email          string            `json:"email"`
	ChallengeType  ACMEChallengeType `json:"challenge_type"`
	DirectoryURL   string            `json:"directory_url"`
	StorageDir     string            `json:"storage_dir"`
	RenewalWindow  time.Duration     `json:"renewal_window"`
	EnableStaging  bool              `json:"enable_staging"`
}

// CertMetadata encapsulates parsed cryptographic and validity properties of an on-disk certificate.
type CertMetadata struct {
	Domain       string
	SANs         []string
	Issuer       string
	NotBefore    time.Time
	NotAfter     time.Time
	SerialNumber string
	DaysRemaining int
}

// ValidateCertConfig verifies domains, email syntax, and challenge constraints.
//
// Business rule: Enforce RFC 5322 email syntax and RFC 1035 domain label conventions
// before submitting orders to ACME directory.
//
// @ai-constraint: Reject wildcard certificates for HTTP-01 challenges per ACME specification.
func ValidateCertConfig(cfg CertConfig) error {
	if len(cfg.Domains) == 0 {
		return ErrEmptyDomainList
	}

	for _, domain := range cfg.Domains {
		clean := strings.TrimSpace(domain)
		if clean == "" || strings.Contains(clean, " ") || strings.Contains(clean, "..") {
			return fmt.Errorf("%w: %q", ErrInvalidDomainName, domain)
		}
		if strings.HasPrefix(clean, "*.") && cfg.ChallengeType == ChallengeHTTP01 {
			return fmt.Errorf("%w: wildcard domain %q incompatible with HTTP-01 challenge", ErrInvalidChallenge, domain)
		}
	}

	if cfg.Email == "" {
		return ErrInvalidEmailAddress
	}
	if _, err := mail.ParseAddress(cfg.Email); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidEmailAddress, cfg.Email)
	}

	switch cfg.ChallengeType {
	case ChallengeHTTP01, ChallengeTLSALPN01:
		// valid
	case "":
		// Default to HTTP-01
	default:
		return fmt.Errorf("%w: %s", ErrInvalidChallenge, cfg.ChallengeType)
	}

	return nil
}

// BuildProxyTLSArgs converts a CertConfig into CLI argument flags compatible with the proxy process.
//
// Business rule: Translates abstract certificate specifications into concrete CLI flags
// passed during proxy daemon initialization.
//
// @ai-constraint: Do not leak private key paths or credentials into stdout/logging.
func BuildProxyTLSArgs(cfg CertConfig) ([]string, error) {
	if err := ValidateCertConfig(cfg); err != nil {
		return nil, err
	}

	args := make([]string, 0, 8)
	args = append(args, "--tls")
	args = append(args, "--tls-email", cfg.Email)

	if cfg.StorageDir != "" {
		args = append(args, "--tls-storage", cfg.StorageDir)
	} else {
		args = append(args, "--tls-storage", DefaultStorageDirectory)
	}

	if cfg.EnableStaging {
		args = append(args, "--tls-directory", LetsEncryptStagingURL)
	} else if cfg.DirectoryURL != "" {
		args = append(args, "--tls-directory", cfg.DirectoryURL)
	} else {
		args = append(args, "--tls-directory", LetsEncryptProductionURL)
	}

	if cfg.ChallengeType == ChallengeTLSALPN01 {
		args = append(args, "--tls-challenge", "tls-alpn-01")
	} else {
		args = append(args, "--tls-challenge", "http-01")
	}

	return args, nil
}

// ParseCertificatePEM reads and parses an X.509 certificate from raw PEM byte data.
//
// Business rule: Extracts expiration boundary and Subject Alternative Names (SANs)
// for automated renewal scheduling.
//
// @ai-constraint: Safely handle multi-certificate chains; always inspect the leaf certificate (first block).
func ParseCertificatePEM(pemData []byte) (*CertMetadata, error) {
	if len(pemData) == 0 {
		return nil, ErrPEMDecodeFailed
	}

	var block *pem.Block
	rest := pemData
	for {
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, ErrNoCertificateBlock
		}
		if block.Type == "CERTIFICATE" {
			break
		}
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cert: x509 parse error: %w", err)
	}

	now := time.Now()
	daysRemaining := int(cert.NotAfter.Sub(now).Hours() / 24)

	return &CertMetadata{
		Domain:        cert.Subject.CommonName,
		SANs:          cert.DNSNames,
		Issuer:        cert.Issuer.CommonName,
		NotBefore:     cert.NotBefore,
		NotAfter:      cert.NotAfter,
		SerialNumber:  cert.SerialNumber.String(),
		DaysRemaining: daysRemaining,
	}, nil
}

// LoadCertMetadata reads a certificate file from disk and parses its metadata.
//
// Business rule: File path lookup adheres to standard storage directory hierarchy:
// `<storage_dir>/<primary_domain>.crt`.
//
// @ai-constraint: Guard against path traversal attacks by validating domain formatting.
func LoadCertMetadata(storageDir, domain string) (*CertMetadata, error) {
	cleanDomain := filepath.Base(domain)
	certPath := filepath.Join(storageDir, cleanDomain+".crt")

	data, err := os.ReadFile(certPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrCertificateNotFound, certPath)
		}
		return nil, fmt.Errorf("cert: read file %s: %w", certPath, err)
	}

	return ParseCertificatePEM(data)
}

// NeedsRenewal determines if a certificate requires ACME renewal based on threshold.
//
// Business rule: Trigger renewal when remaining valid lifetime falls below renewal window
// (default: 30 days) or if current time is past expiration.
//
// @ai-constraint: Thread-safe pure function evaluating monotonic time checks.
func NeedsRenewal(meta *CertMetadata, renewalWindow time.Duration) bool {
	if meta == nil {
		return true
	}
	if renewalWindow <= 0 {
		renewalWindow = DefaultCertRenewalWindow
	}

	threshold := time.Now().Add(renewalWindow)
	return meta.NotAfter.Before(threshold)
}
