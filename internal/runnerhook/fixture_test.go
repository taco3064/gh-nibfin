package runnerhook_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/taco3064/gh-nibfin/internal/runnerhook"
)

// The signature fixtures: a test Fulcio (root and intermediate), a test Rekor
// key and leaves with Fulcio's extensions, all generated per test.

var (
	signedAt   = time.Date(2026, 11, 25, 15, 44, 0, 0, time.UTC)
	signerURI  = "https://github.com/" + runnerhook.SignerWorkflow + "@" + runnerhook.SourceRef
	fulcioBase = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1}
)

type ca struct {
	rootKey, interKey   *ecdsa.PrivateKey
	root, inter         *x509.Certificate
	rootDER, interDER   []byte
	rekorKey            *ecdsa.PrivateKey
	rekorSPKI, rekorKID []byte
}

func mustKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustCert(t *testing.T, tmpl, parent *x509.Certificate, pub, priv any) ([]byte, *x509.Certificate) {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return der, c
}

func newCA(t *testing.T) *ca {
	t.Helper()
	c := &ca{rootKey: mustKey(t), interKey: mustKey(t), rekorKey: mustKey(t)}
	from, to := time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC)
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{Organization: []string{"sigstore.dev"}, CommonName: "sigstore"},
		NotBefore: from, NotAfter: to, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	c.rootDER, c.root = mustCert(t, root, root, &c.rootKey.PublicKey, c.rootKey)
	inter := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{Organization: []string{"sigstore.dev"}, CommonName: "sigstore-intermediate"},
		NotBefore: from, NotAfter: to, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	c.interDER, c.inter = mustCert(t, inter, c.root, &c.interKey.PublicKey, c.rootKey)
	var err error
	if c.rekorSPKI, err = x509.MarshalPKIXPublicKey(&c.rekorKey.PublicKey); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(c.rekorSPKI)
	c.rekorKID = sum[:]
	return c
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// trustedRoot is the trusted root naming c, in gh attestation trusted-root's
// form.
func (c *ca) trustedRoot() []byte {
	root := map[string]any{
		"mediaType": "application/vnd.dev.sigstore.trustedroot+json;version=0.1",
		"tlogs": []any{map[string]any{
			"baseUrl": "https://rekor.sigstore.dev", "hashAlgorithm": "SHA2_256",
			"publicKey": map[string]any{"rawBytes": b64(c.rekorSPKI), "keyDetails": "PKIX_ECDSA_P256_SHA_256",
				"validFor": map[string]any{"start": "2021-01-01T00:00:00Z"}},
			"logId": map[string]any{"keyId": b64(c.rekorKID)},
		}},
		"certificateAuthorities": []any{map[string]any{
			"subject":   map[string]any{"organization": "sigstore.dev", "commonName": "sigstore"},
			"uri":       "https://fulcio.sigstore.dev",
			"certChain": map[string]any{"certificates": []any{map[string]any{"rawBytes": b64(c.interDER)}, map[string]any{"rawBytes": b64(c.rootDER)}}},
			"validFor":  map[string]any{"start": "2022-04-13T20:06:15Z"},
		}},
	}
	b, _ := json.Marshal(root)
	return append(b, '\n')
}

// bundleOpts change one aspect of a generated bundle.
type bundleOpts struct {
	signer, san, sourceRepo, ref, runner, issuer string
	// sourceDigest is the Source Repository Digest (extension 13); empty
	// means commit, noSourceDigest leaves the extension out.
	sourceDigest   string
	noSourceDigest bool
	// at is the log time T; zero means signedAt.
	at time.Time
	// issuerCA signs the leaf instead of the intermediate.
	issuerCA *ca
	// rekor signs the entry timestamp instead of the CA's Rekor key.
	rekor *ecdsa.PrivateKey
	// badSig breaks the envelope signature; badSET the entry timestamp.
	badSig, badSET bool
	// bodyPayload and bodySig replace what the log entry names.
	bodyPayload []byte
	bodySig     []byte
	// statement replaces the statement fields.
	subjectName, predicateType string
	digest                     string
	noTlog                     bool
}

func ext(t *testing.T, n int, v string) pkix.Extension {
	t.Helper()
	b, err := asn1.MarshalWithParams(v, "utf8")
	if err != nil {
		t.Fatal(err)
	}
	return pkix.Extension{Id: append(append(asn1.ObjectIdentifier{}, fulcioBase...), n), Value: b}
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// bundle returns a Sigstore bundle v0.3 for digest as nibfin-core's image
// workflow produces it, changed by o.
func (c *ca) bundle(t *testing.T, digest string, o bundleOpts) map[string]any {
	t.Helper()
	at := o.at
	if at.IsZero() {
		at = signedAt
	}
	issuer := c
	if o.issuerCA != nil {
		issuer = o.issuerCA
	}
	leafKey := mustKey(t)
	san, err := urlOf(or(o.san, signerURI))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		NotBefore:    signedAt.Add(-time.Minute), NotAfter: signedAt.Add(9 * time.Minute),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs: san,
		ExtraExtensions: []pkix.Extension{
			ext(t, 8, or(o.issuer, "https://token.actions.githubusercontent.com")),
			ext(t, 9, or(o.signer, signerURI)),
			ext(t, 11, or(o.runner, "github-hosted")),
			ext(t, 12, or(o.sourceRepo, "https://github.com/"+runnerhook.SourceRepo)),
			ext(t, 14, or(o.ref, runnerhook.SourceRef)),
		},
	}
	if !o.noSourceDigest {
		tmpl.ExtraExtensions = append(tmpl.ExtraExtensions, ext(t, 13, or(o.sourceDigest, commit)))
	}
	leafDER, _ := mustCert(t, tmpl, issuer.inter, &leafKey.PublicKey, issuer.interKey)

	statement, _ := json.Marshal(map[string]any{
		"_type":         "https://in-toto.io/Statement/v1",
		"subject":       []any{map[string]any{"name": or(o.subjectName, runnerhook.ImageRepository), "digest": map[string]string{"sha256": or(o.digest, digest)}}},
		"predicateType": or(o.predicateType, "https://slsa.dev/provenance/v1"),
		"predicate":     map[string]any{"buildDefinition": map[string]any{}},
	})
	payloadType := "application/vnd.in-toto+json"
	pae := fmt.Sprintf("DSSEv1 %d %s %d ", len(payloadType), payloadType, len(statement)) + string(statement)
	h := sha256.Sum256([]byte(pae))
	sig, err := ecdsa.SignASN1(rand.Reader, leafKey, h[:])
	if err != nil {
		t.Fatal(err)
	}
	if o.badSig {
		other := mustKey(t)
		sig, _ = ecdsa.SignASN1(rand.Reader, other, h[:])
	}
	bodyPayload, bodySig := statement, sig
	if o.bodyPayload != nil {
		bodyPayload = o.bodyPayload
	}
	if o.bodySig != nil {
		bodySig = o.bodySig
	}
	ph := sha256.Sum256(bodyPayload)
	body, _ := json.Marshal(map[string]any{
		"apiVersion": "0.0.1", "kind": "dsse",
		"spec": map[string]any{
			"envelopeHash": map[string]string{"algorithm": "sha256", "value": hex.EncodeToString(ph[:])},
			"payloadHash":  map[string]string{"algorithm": "sha256", "value": hex.EncodeToString(ph[:])},
			"signatures": []any{map[string]string{
				"signature": b64(bodySig),
				"verifier":  b64(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})),
			}},
		},
	})
	cb := b64(body)
	logIndex := "155690850"
	signed := fmt.Sprintf(`{"body":%q,"integratedTime":%d,"logID":"%s","logIndex":%s}`, cb, at.Unix(), hex.EncodeToString(c.rekorKID), logIndex)
	rk := c.rekorKey
	if o.rekor != nil {
		rk = o.rekor
	}
	sh := sha256.Sum256([]byte(signed))
	set, err := ecdsa.SignASN1(rand.Reader, rk, sh[:])
	if err != nil {
		t.Fatal(err)
	}
	if o.badSET {
		set[len(set)-1] ^= 1
	}
	entries := []any{map[string]any{
		"logIndex": logIndex, "logId": map[string]string{"keyId": b64(c.rekorKID)},
		"kindVersion": map[string]string{"kind": "dsse", "version": "0.0.1"}, "integratedTime": fmt.Sprint(at.Unix()),
		"inclusionPromise":  map[string]string{"signedEntryTimestamp": b64(set)},
		"inclusionProof":    map[string]any{"logIndex": "33786588", "treeSize": "33786590"},
		"canonicalizedBody": cb,
	}}
	if o.noTlog {
		entries = []any{}
	}
	return map[string]any{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": map[string]any{
			"certificate":               map[string]string{"rawBytes": b64(leafDER)},
			"tlogEntries":               entries,
			"timestampVerificationData": map[string]any{},
		},
		"dsseEnvelope": map[string]any{
			"payload": b64(statement), "payloadType": payloadType,
			"signatures": []any{map[string]string{"sig": b64(sig), "keyid": ""}},
		},
	}
}

// attestations is the REST answer listing bundles.
func attestations(bundles ...map[string]any) []byte {
	list := []any{}
	for _, b := range bundles {
		list = append(list, map[string]any{"bundle": b, "repository_id": 1})
	}
	out, _ := json.Marshal(map[string]any{"attestations": list})
	return out
}
