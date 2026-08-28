Pinned root CA(s) for CAP `<Signature>` verification (`internal/capverify`).

`cap_trust_roots.pem` currently contains:
- **IdenTrust Global Common Root CA 1** (self-signed) — the root of the chain
  FEMA IPAWS device certificates (e.g. `IPAWSOPEN200300`) issue from, via the
  intermediate "IGC Device CA 2".

## Why only the root is pinned here

Intermediate certs (like "IGC Device CA 2") rotate periodically — IdenTrust
has already replaced "IGC Device CA 1" once. `capverify.Verifier` fetches
whichever intermediate is currently in use automatically, from the URL in the
leaf certificate's own Authority Information Access "CA Issuers" extension
(cached in memory). That's safe because the fetched intermediate still has to
chain back to a root pinned here — an attacker controlling the AIA response
can't get past that.

The root itself is **not** auto-fetched from anything the alert message
could influence. It must be obtained out-of-band and updated deliberately.

## How to update / verify this file

Fetched 2026-08-28 from IdenTrust's published AIA chain for a live IPAWS-OPEN
device cert (`validation.identrust.com/certs/igcdeviceca2.p7c`), which
resolves to this root. To refresh or independently verify:

```
# from a leaf cert's AIA "CA Issuers" URL (openssl x509 -text -noout -in leaf.pem)
curl -o issuer.p7c http://validation.identrust.com/certs/igcdeviceca2.p7c
openssl pkcs7 -inform DER -in issuer.p7c -print_certs -out chain.pem
# chain.pem contains: IGC Device CA 2 (intermediate) + IdenTrust Global Common Root CA 1 (self-signed)
# the self-signed one (subject == issuer) is what belongs in cap_trust_roots.pem
```

SHA-256 fingerprint of the pinned root (verify against IdenTrust's own
published fingerprint before trusting a refreshed copy):
```
09:B1:5A:D8:D0:CA:03:28:61:89:2E:55:E7:46:AE:8D:AF:1B:FD:B5:3A:9E:5A:EE:81:37:D6:F8:9A:A1:11:13
```

Additional roots can be appended to the same PEM file (one `BEGIN/END
CERTIFICATE` block each) if other CAP sources sign with a different CA.
