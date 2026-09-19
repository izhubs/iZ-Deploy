// Package proxy verifies ACME certificate configuration, PEM decoding, and renewal heuristics.
package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// generateTestCertificate generates an in-memory X.509 certificate PEM block for testing.
func generateTestCertificate(commonName string, sans []string, validDuration time.Duration) ([]byte, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	notBefore := time.Now().Add(-1 * time.Hour)
	notAfter := notBefore.Add(validDuration)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"iZdeploy Test Suite"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              sans,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return nil, err
	}

	var pemBuf []byte
	pemBuf = append(pemBuf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})...)
	return pemBuf, nil
}

// TestValidateCertConfig verifies validation constraints across domains, emails, and ACME challenge protocols.
//
// Business rule: Enforce strict RFC compliance for domain labels and contact email addresses.
//
// @ai-constraint: Guard against wildcard HTTP-01 challenge combinations.
func TestValidateCertConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CertConfig
		wantErr bool
		errType error
	}{
		{
			name: "Valid HTTP-01 Config",
			cfg: CertConfig{
				Domains:       []string{"example.com", "app.example.com"},
				Email:         "admin@example.com",
				ChallengeType: ChallengeHTTP01,
			},
			wantErr: false,
		},
		{
			name: "Valid TLS-ALPN-01 Config",
			cfg: CertConfig{
				Domains:       []string{"secure.example.com"},
				Email:         "ops@example.com",
				ChallengeType: ChallengeTLSALPN01,
			},
			wantErr: false,
		},
		{
			name: "Empty Domains",
			cfg: CertConfig{
				Domains: []string{},
				Email:   "admin@example.com",
			},
			wantErr: true,
			errType: ErrEmptyDomainList,
		},
		{
			name: "Invalid Domain Name with Spaces",
			cfg: CertConfig{
				Domains: []string{"invalid domain.com"},
				Email:   "admin@example.com",
			},
			wantErr: true,
			errType: ErrInvalidDomainName,
		},
		{
			name: "Wildcard with HTTP-01 Disallowed",
			cfg: CertConfig{
				Domains:       []string{"*.example.com"},
				Email:         "admin@example.com",
				ChallengeType: ChallengeHTTP01,
			},
			wantErr: true,
			errType: ErrInvalidChallenge,
		},
		{
			name: "Invalid Email Address",
			cfg: CertConfig{
				Domains: []string{"example.com"},
				Email:   "not-an-email",
			},
			wantErr: true,
			errType: ErrInvalidEmailAddress,
		},
		{
			name: "Unsupported Challenge Type",
			cfg: CertConfig{
				Domains:       []string{"example.com"},
				Email:         "admin@example.com",
				ChallengeType: "DNS-01",
			},
			wantErr: true,
			errType: ErrInvalidChallenge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCertConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateCertConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errType != nil && !errors.Is(err, tt.errType) {
				t.Errorf("ValidateCertConfig() error = %v, expected error type %v", err, tt.errType)
			}
		})
	}
}

// TestBuildProxyTLSArgs verifies CLI argument rendering for proxy process invocation.
//
// Business rule: Arguments must include email, storage, directory URL, and challenge type.
//
// @ai-constraint: Ensure staging directory URL is selected when EnableStaging is true.
func TestBuildProxyTLSArgs(t *testing.T) {
	cfgProd := CertConfig{
		Domains:       []string{"app.example.com"},
		Email:         "ops@example.com",
		ChallengeType: ChallengeHTTP01,
		StorageDir:    "/var/certs",
	}

	args, err := BuildProxyTLSArgs(cfgProd)
	if err != nil {
		t.Fatalf("BuildProxyTLSArgs() failed: %v", err)
	}

	expectedProd := map[string]string{
		"--tls":           "",
		"--tls-email":     "ops@example.com",
		"--tls-storage":   "/var/certs",
		"--tls-directory": LetsEncryptProductionURL,
		"--tls-challenge": "http-01",
	}

	argMap := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			argMap[args[i]] = args[i+1]
			i++
		} else {
			argMap[args[i]] = ""
		}
	}

	for k, v := range expectedProd {
		val, ok := argMap[k]
		if !ok {
			t.Errorf("expected argument %s present in args", k)
		} else if v != "" && val != v {
			t.Errorf("expected %s=%s, got %s", k, v, val)
		}
	}

	// Test staging directory
	cfgStaging := CertConfig{
		Domains:       []string{"test.example.com"},
		Email:         "dev@example.com",
		EnableStaging: true,
	}
	stagingArgs, err := BuildProxyTLSArgs(cfgStaging)
	if err != nil {
		t.Fatalf("BuildProxyTLSArgs(staging) failed: %v", err)
	}

	foundStaging := false
	for i, arg := range stagingArgs {
		if arg == "--tls-directory" && i+1 < len(stagingArgs) && stagingArgs[i+1] == LetsEncryptStagingURL {
			foundStaging = true
			break
		}
	}
	if !foundStaging {
		t.Errorf("expected staging directory URL in args: %+v", stagingArgs)
	}
}

// TestParseCertificatePEM_And_NeedsRenewal verifies parsing and renewal heuristics.
//
// Business rule: Certificates expiring within renewal window (<30 days) must trigger renewal.
//
// @ai-constraint: Pure monotonic time calculations.
func TestParseCertificatePEM_And_NeedsRenewal(t *testing.T) {
	// 1. Generate certificate expiring in 15 days
	pem15Days, err := generateTestCertificate("expiring.example.com", []string{"expiring.example.com"}, 15*24*time.Hour)
	if err != nil {
		t.Fatalf("generateTestCertificate failed: %v", err)
	}

	meta15, err := ParseCertificatePEM(pem15Days)
	if err != nil {
		t.Fatalf("ParseCertificatePEM() failed: %v", err)
	}
	if meta15.Domain != "expiring.example.com" {
		t.Errorf("expected domain expiring.example.com, got %s", meta15.Domain)
	}

	if !NeedsRenewal(meta15, 30*24*time.Hour) {
		t.Errorf("expected certificate expiring in 15 days to need renewal (window=30 days)")
	}

	// 2. Generate certificate valid for 90 days
	pem90Days, err := generateTestCertificate("valid.example.com", []string{"valid.example.com"}, 90*24*time.Hour)
	if err != nil {
		t.Fatalf("generateTestCertificate failed: %v", err)
	}

	meta90, err := ParseCertificatePEM(pem90Days)
	if err != nil {
		t.Fatalf("ParseCertificatePEM() failed: %v", err)
	}

	if NeedsRenewal(meta90, 30*24*time.Hour) {
		t.Errorf("expected certificate valid for 90 days NOT to need renewal (window=30 days)")
	}

	// 3. Nil metadata defaults to needing renewal
	if !NeedsRenewal(nil, 30*24*time.Hour) {
		t.Errorf("expected nil metadata to require renewal")
	}
}

// TestLoadCertMetadata verifies disk certificate reading and error handling.
//
// Business rule: Missing certificate files must return ErrCertificateNotFound.
//
// @ai-constraint: Use t.TempDir for filesystem isolation.
func TestLoadCertMetadata(t *testing.T) {
	tempDir := t.TempDir()

	pemData, err := generateTestCertificate("site.example.com", []string{"site.example.com"}, 60*24*time.Hour)
	if err != nil {
		t.Fatalf("generateTestCertificate failed: %v", err)
	}

	certPath := filepath.Join(tempDir, "site.example.com.crt")
	if err := os.WriteFile(certPath, pemData, 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	meta, err := LoadCertMetadata(tempDir, "site.example.com")
	if err != nil {
		t.Fatalf("LoadCertMetadata() failed: %v", err)
	}
	if meta.Domain != "site.example.com" {
		t.Errorf("expected domain site.example.com, got %s", meta.Domain)
	}

	// Non-existent certificate
	_, errNotFound := LoadCertMetadata(tempDir, "nonexistent.example.com")
	if !errors.Is(errNotFound, ErrCertificateNotFound) {
		t.Errorf("expected ErrCertificateNotFound, got %v", errNotFound)
	}
}
