// Package capverify verifies the XML-DSig <Signature> block that IPAWS and
// other CAP 1.2 sources embed in signed alert messages.
//
// Verification has three parts, all required for Result.Verified to be true:
//  1. Chain of trust — the certificate embedded in the message's KeyInfo must
//     chain to one of the pinned trusted roots. Any missing intermediate is
//     fetched from the leaf certificate's own Authority Information Access
//     "CA Issuers" URL (cached), never from a URL supplied by the message.
//  2. Revocation — a best-effort live OCSP check against the leaf's AIA OCSP
//     URL. An explicit "revoked" response fails verification; an unreachable
//     responder does not (network flakiness shouldn't cause a real alert to
//     be dropped).
//  3. Signature — the enveloped XML-DSig digest and RSA signature must match
//     the alert content, checked against the now-chain-verified certificate.
package capverify

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
	"go.mozilla.org/pkcs7"
	"golang.org/x/crypto/ocsp"
)

// Result describes the outcome of verifying one CAP alert's signature.
type Result struct {
	Signed   bool   // an enveloped <Signature> element was present
	Verified bool   // signature + chain + (best-effort) revocation all checked out
	Revoked  bool   // true only when a live OCSP check explicitly reported the cert revoked
	Reason   string // human-readable explanation when Verified is false
}

// Status returns a short machine-readable label for storage/display.
func (r Result) Status() string {
	switch {
	case !r.Signed:
		return "unsigned"
	case r.Revoked:
		return "revoked"
	case r.Verified:
		return "verified"
	default:
		return "invalid"
	}
}

type cachedCert struct {
	cert    *x509.Certificate
	fetched time.Time
}

// Verifier validates CAP alert XML-DSig signatures against a pinned set of
// trusted root certificates.
type Verifier struct {
	roots  *x509.CertPool
	client *http.Client
	ttl    time.Duration

	mu    sync.Mutex
	cache map[string]cachedCert // keyed by AIA "CA Issuers" URL
}

// NewVerifier builds a Verifier trusting only the roots in trustedRootsPEM
// (a PEM bundle of one or more CA certificates).
func NewVerifier(trustedRootsPEM []byte) (*Verifier, error) {
	pool := x509.NewCertPool()
	if ok := pool.AppendCertsFromPEM(trustedRootsPEM); !ok {
		return nil, errors.New("capverify: no valid certificates found in trusted roots file")
	}
	return &Verifier{
		roots:  pool,
		client: &http.Client{Timeout: 10 * time.Second},
		ttl:    24 * time.Hour,
		cache:  make(map[string]cachedCert),
	}, nil
}

var whiteSpaceRe = regexp.MustCompile(`\s+`)

// Verify parses rawXML for an enveloped XML-DSig <Signature>, verifies its
// embedded certificate chains to a trusted root, performs a best-effort OCSP
// revocation check, and verifies the digest + signature against the alert
// content.
func (v *Verifier) Verify(ctx context.Context, rawXML []byte) Result {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(rawXML); err != nil {
		return Result{Reason: "parse XML: " + err.Error()}
	}
	root := doc.Root()
	if root == nil {
		return Result{Reason: "empty document"}
	}

	certEl := root.FindElement(".//KeyInfo/X509Data/X509Certificate")
	if certEl == nil {
		return Result{Signed: false, Reason: "no signature present"}
	}

	certDER, err := base64.StdEncoding.DecodeString(whiteSpaceRe.ReplaceAllString(certEl.Text(), ""))
	if err != nil {
		return Result{Signed: true, Reason: "decode embedded certificate: " + err.Error()}
	}
	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return Result{Signed: true, Reason: "parse embedded certificate: " + err.Error()}
	}

	intermediates, err := v.resolveIntermediates(ctx, leaf)
	if err != nil {
		return Result{Signed: true, Reason: "resolve certificate chain: " + err.Error()}
	}

	chains, err := leaf.Verify(x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return Result{Signed: true, Reason: "certificate chain: " + err.Error()}
	}

	if revoked := v.checkRevocation(ctx, leaf, chains[0]); revoked {
		return Result{Signed: true, Revoked: true, Reason: "certificate revoked (OCSP)"}
	}

	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{leaf}}
	valCtx := dsig.NewDefaultValidationContext(store)
	if _, err := valCtx.Validate(root); err != nil {
		return Result{Signed: true, Reason: "signature: " + err.Error()}
	}

	return Result{Signed: true, Verified: true}
}

// resolveIntermediates walks the leaf's Authority Information Access "CA
// Issuers" URL chain (never a URL supplied by the message being verified)
// until it reaches a pinned root or runs out of AIA pointers.
func (v *Verifier) resolveIntermediates(ctx context.Context, leaf *x509.Certificate) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	seen := map[string]bool{}

	cert := leaf
	for depth := 0; depth < 5; depth++ {
		opts := x509.VerifyOptions{Roots: v.roots, Intermediates: pool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}
		if _, err := cert.Verify(opts); err == nil {
			return pool, nil
		}
		if len(cert.IssuingCertificateURL) == 0 {
			break
		}
		url := cert.IssuingCertificateURL[0]
		if seen[url] {
			break
		}
		seen[url] = true

		issuer, err := v.fetchIntermediate(ctx, url)
		if err != nil {
			return nil, err
		}
		pool.AddCert(issuer)
		cert = issuer
	}
	// Let the caller's real Verify() call produce the final, precise error.
	return pool, nil
}

func (v *Verifier) fetchIntermediate(ctx context.Context, url string) (*x509.Certificate, error) {
	v.mu.Lock()
	if c, ok := v.cache[url]; ok && time.Since(c.fetched) < v.ttl {
		v.mu.Unlock()
		return c.cert, nil
	}
	v.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	cert, err := parseCertBytes(body)
	if err != nil {
		return nil, fmt.Errorf("parse certificate from %s: %w", url, err)
	}

	v.mu.Lock()
	v.cache[url] = cachedCert{cert: cert, fetched: time.Now()}
	v.mu.Unlock()
	return cert, nil
}

// parseCertBytes accepts a raw DER cert, a PEM cert, or a PKCS#7 "certs-only"
// bundle — the format most public CAs (including IdenTrust) serve their AIA
// "CA Issuers" responses in — and returns the first certificate found.
func parseCertBytes(body []byte) (*x509.Certificate, error) {
	if block, _ := pem.Decode(body); block != nil {
		return x509.ParseCertificate(block.Bytes)
	}
	if cert, err := x509.ParseCertificate(body); err == nil {
		return cert, nil
	}
	p7, err := pkcs7.Parse(body)
	if err != nil {
		return nil, err
	}
	if len(p7.Certificates) == 0 {
		return nil, errors.New("no certificates found in response")
	}
	return p7.Certificates[0], nil
}

// checkRevocation performs a best-effort live OCSP check. It returns true
// only when the responder explicitly reports the certificate revoked;
// any other outcome (no OCSP URL, unreachable responder, malformed
// response) is treated as non-fatal so a transient network issue can't
// cause a legitimate alert to be dropped.
func (v *Verifier) checkRevocation(ctx context.Context, leaf *x509.Certificate, chain []*x509.Certificate) bool {
	if len(leaf.OCSPServer) == 0 || len(chain) < 2 {
		return false
	}
	issuer := chain[1]

	reqBytes, err := ocsp.CreateRequest(leaf, issuer, nil)
	if err != nil {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, leaf.OCSPServer[0], bytes.NewReader(reqBytes))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/ocsp-request")

	resp, err := v.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false
	}

	ocspResp, err := ocsp.ParseResponseForCert(body, leaf, issuer)
	if err != nil {
		return false
	}
	return ocspResp.Status == ocsp.Revoked
}
