package capverify

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// realAlabamaRWTCancel is a genuine signed IPAWS-OPEN CAP message (an
// Alabama EMA "Required Weekly Test" cancellation), used to exercise the
// verifier end-to-end against the live IdenTrust AIA/OCSP infrastructure.
const realAlabamaRWTCancel = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><alert xmlns="urn:oasis:names:tc:emergency:cap:1.2"><identifier>6a90b3de3fedd9125cff728b</identifier><sender>westly.martin@ema.alabama.gov</sender><sent>2026-08-27T17:02:06-05:00</sent><status>Actual</status><msgType>Cancel</msgType><source>071502v1</source><scope>Public</scope><code>IPAWSv1.0</code><references>westly.martin@ema.alabama.gov,6a90b38a3fedd9125cff7287,2026-08-27T17:01:25-05:00</references><info><language>en-US</language><category>Safety</category><event>Required Weekly Test</event><responseType>Shelter</responseType><urgency>Immediate</urgency><severity>Extreme</severity><certainty>Observed</certainty><eventCode><valueName>SAME</valueName><value>RWT</value></eventCode><expires>2026-08-27T17:16:25-05:00</expires><senderName>Alabama Emergency Management Agency</senderName><headline>Cancellation: Alabama EMA Required Weekly Test</headline><description>Cancellation: Alabama EMA Required Weekly Test</description><parameter><valueName>EAS-ORG</valueName><value>CIV</value></parameter><parameter><valueName>TimeZone</valueName><value>CST</value></parameter><parameter><valueName>BLOCKCHANNEL</valueName><value>CMAS</value></parameter><parameter><valueName>BLOCKCHANNEL</valueName><value>NWEM</value></parameter><area><areaDesc>Alabama State Wide</areaDesc><geocode><valueName>SAME</valueName><value>001000</value></geocode></area></info><Signature xmlns="http://www.w3.org/2000/09/xmldsig#"><SignedInfo><CanonicalizationMethod Algorithm="http://www.w3.org/2001/10/xml-exc-c14n#"/><SignatureMethod Algorithm="http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"/><Reference URI=""><Transforms><Transform Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature"/></Transforms><DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"/><DigestValue>W3lcFmantTHiz1iJ22WE1oO84RCgtwZYln2z6bqSqdI=</DigestValue></Reference></SignedInfo><SignatureValue>bMsYUHXBstLPf9zF2knJz+ICe8unQLMe3H/csGKUtIAGD4Y/kSv/N5kOjj3WTpiFFfOCdYqEmbRNVCYAppV7tCAKUx0Ty0xUrcm3tDCnwPVX6E63IYtVvwLaH5nC/Jzh9idbMjEU5/ULblMZEF1B0/Zkt7sVIBsi64U/yW8uXdojtMKIjI+gpwhsru//mBL3GPSJ9sUahWvCLy5v+IyMeE5GXKJk3/hU5URIzu6QAy47SlTOdldrSpjv1P0zHHlOJp+oaIPiMS/OccvdaZIydr1ZENI+bOjvVGBP6y94LxBm0Y1OVRtvJ5X2wEiSkrV3w9+DiRPr9xgn1YLXU7/7cg==</SignatureValue><KeyInfo><X509Data><X509SubjectName>CN=IPAWSOPEN200300,OU=Devices,OU=FEMA,OU=Department of Homeland Security,O=U.S. Government,C=US</X509SubjectName><X509Certificate>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</X509Certificate></X509Data></KeyInfo></Signature></alert>`

func loadTestVerifier(t *testing.T) *Verifier {
	t.Helper()
	pemBytes, err := os.ReadFile("../../config/tls/cap_trust_roots.pem")
	if err != nil {
		t.Fatalf("read trust roots: %v", err)
	}
	v, err := NewVerifier(pemBytes)
	if err != nil {
		t.Fatalf("new verifier: %v", err)
	}
	return v
}

// requireNetwork skips the test if the live AIA/OCSP infrastructure this
// verifier depends on isn't reachable from the test environment.
func requireNetwork(t *testing.T, v *Verifier) {
	t.Helper()
	if _, err := v.fetchIntermediate(context.Background(), "http://validation.identrust.com/certs/igcdeviceca2.p7c"); err != nil {
		t.Skipf("network/AIA endpoint unreachable, skipping live verification test: %v", err)
	}
}

func TestVerify_RealSignedAlert(t *testing.T) {
	v := loadTestVerifier(t)
	requireNetwork(t, v)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	result := v.Verify(ctx, []byte(realAlabamaRWTCancel))
	if !result.Signed {
		t.Fatalf("expected Signed=true, got Result=%+v", result)
	}
	if !result.Verified {
		t.Fatalf("expected a genuine IPAWS-signed alert to verify, got Result=%+v", result)
	}
	if got := result.Status(); got != "verified" {
		t.Errorf("Status() = %q, want %q", got, "verified")
	}
}

func TestVerify_TamperedContentFailsSignature(t *testing.T) {
	v := loadTestVerifier(t)
	requireNetwork(t, v)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tampered := strings.Replace(realAlabamaRWTCancel, "Alabama State Wide", "Alabama State Wide!", 1)

	result := v.Verify(ctx, []byte(tampered))
	if result.Verified {
		t.Fatalf("expected tampered alert content to fail verification, got Result=%+v", result)
	}
	if got := result.Status(); got != "invalid" {
		t.Errorf("Status() = %q, want %q", got, "invalid")
	}
}

func TestVerify_UnsignedMessage(t *testing.T) {
	v := loadTestVerifier(t)

	unsigned := `<?xml version="1.0"?><alert xmlns="urn:oasis:names:tc:emergency:cap:1.2"><identifier>test-1</identifier></alert>`
	result := v.Verify(context.Background(), []byte(unsigned))
	if result.Signed {
		t.Fatalf("expected Signed=false for a message with no <Signature>, got Result=%+v", result)
	}
	if got := result.Status(); got != "unsigned" {
		t.Errorf("Status() = %q, want %q", got, "unsigned")
	}
}

func TestNewVerifier_RejectsEmptyRoots(t *testing.T) {
	if _, err := NewVerifier([]byte("not a certificate")); err == nil {
		t.Fatal("expected an error constructing a Verifier with no valid root certs")
	}
}
