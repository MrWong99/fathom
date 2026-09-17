// Copyright 2026 Lukas Schmidt
// SPDX-License-Identifier: MIT

package render

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"text/template"
	"time"
)

// Certificate mirrors the unexported struct sprig returns from genCA,
// genSelfSignedCert and genSignedCert; templates read .Cert and .Key.
type Certificate struct {
	Cert string
	Key  string
}

// stream is a counter-mode SHA-256 byte stream seeded from the report inputs.
// Helm renders templates in a sorted order (engine.sortTemplates breaks ties
// by name) and executes each template sequentially, so a shared counter is
// deterministic for identical inputs while still giving distinct values to
// distinct calls, which is what charts expect from two randAlphaNum calls.
type stream struct {
	seed    [32]byte
	counter uint64
	buf     []byte
}

func newStream(seed string) *stream {
	return &stream{seed: sha256.Sum256([]byte("fathom-render-seed:" + seed))}
}

func (s *stream) Read(p []byte) (int, error) {
	for len(s.buf) < len(p) {
		var block [40]byte
		copy(block[:32], s.seed[:])
		binary.BigEndian.PutUint64(block[32:], s.counter)
		s.counter++
		sum := sha256.Sum256(block[:])
		s.buf = append(s.buf, sum[:]...)
	}
	n := copy(p, s.buf[:len(p)])
	s.buf = s.buf[n:]
	return n, nil
}

func (s *stream) bytes(n int) []byte {
	b := make([]byte, n)
	_, _ = s.Read(b)
	return b
}

func (s *stream) chars(n int, alphabet string) string {
	raw := s.bytes(n)
	out := make([]byte, n)
	for i, c := range raw {
		out[i] = alphabet[int(c)%len(alphabet)]
	}
	return string(out)
}

const (
	alphaNum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	alpha    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	numeric  = "0123456789"
)

// DeterministicFuncs returns replacements for every sprig/Helm template
// function whose output depends on entropy or the wall clock. Installed
// through engine.Engine.CustomTemplateFuncs they override the stock functions
// (the engine copies CustomTemplateFuncs last). derivePassword is not
// replaced: sprig's implementation is already deterministic (scrypt over its
// arguments), which the tests verify.
func DeterministicFuncs(seed string, now time.Time) template.FuncMap {
	s := newStream(seed)
	ascii := make([]byte, 0, 95)
	for c := byte(32); c < 127; c++ {
		ascii = append(ascii, c)
	}
	return template.FuncMap{
		"randAlphaNum": func(n int) string { return s.chars(n, alphaNum) },
		"randAlpha":    func(n int) string { return s.chars(n, alpha) },
		"randNumeric":  func(n int) string { return s.chars(n, numeric) },
		"randAscii":    func(n int) string { return s.chars(n, string(ascii)) },
		"randBytes": func(n int) (string, error) {
			return base64.StdEncoding.EncodeToString(s.bytes(n)), nil
		},
		"randInt": func(minimum, maximum int) int {
			if maximum <= minimum {
				return minimum
			}
			return minimum + int(binary.BigEndian.Uint32(s.bytes(4))%uint32(maximum-minimum))
		},
		"shuffle": func(str string) string {
			r := []rune(str)
			for i := len(r) - 1; i > 0; i-- {
				j := int(binary.BigEndian.Uint32(s.bytes(4)) % uint32(i+1))
				r[i], r[j] = r[j], r[i]
			}
			return string(r)
		},
		"uuidv4": func() string {
			b := s.bytes(16)
			b[6] = (b[6] & 0x0f) | 0x40
			b[8] = (b[8] & 0x3f) | 0x80
			return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		},
		"now": func() time.Time { return now },
		"ago": func(t time.Time) string { return now.Sub(t).Round(time.Second).String() },
		// sprig's date and htmlDate format in the process's local zone
		// (dateInZone(fmt, t, "Local")), so the same render differs between a
		// laptop in Europe/Berlin and a CI runner in UTC. Pin them to UTC.
		"date":     func(fmt string, t any) string { return dateUTC(fmt, t, now) },
		"htmlDate": func(t any) string { return dateUTC("2006-01-02", t, now) },
		"genPrivateKey": func(string) string {
			_, priv := edKey(s)
			return pemKey(priv)
		},
		"genCA": func(cn string, days int) (Certificate, error) {
			return selfSigned(s, now, cn, nil, nil, days, true)
		},
		"genCAWithKey": func(cn string, days int, _ string) (Certificate, error) {
			return selfSigned(s, now, cn, nil, nil, days, true)
		},
		"genSelfSignedCert": func(cn string, ips, dns []any, days int) (Certificate, error) {
			return selfSigned(s, now, cn, ips, dns, days, false)
		},
		"genSelfSignedCertWithKey": func(cn string, ips, dns []any, days int, _ string) (Certificate, error) {
			return selfSigned(s, now, cn, ips, dns, days, false)
		},
		"genSignedCert": func(cn string, ips, dns []any, days int, ca Certificate) (Certificate, error) {
			return signed(s, now, cn, ips, dns, days, ca)
		},
		"genSignedCertWithKey": func(cn string, ips, dns []any, days int, ca Certificate, _ string) (Certificate, error) {
			return signed(s, now, cn, ips, dns, days, ca)
		},
		// htpasswd and bcrypt salt from crypto/rand; the shapes are kept so
		// consumers that pattern-match still work, the hash is not a real bcrypt.
		"htpasswd": func(user, pass string) string {
			return user + ":" + fakeBcrypt(user+":"+pass)
		},
		"bcrypt": func(in string) string { return fakeBcrypt(in) },
	}
}

// dateUTC mirrors sprig's dateInZone argument handling (time.Time, *time.Time,
// int64/int/int32 epoch seconds, anything else = now) but formats in UTC.
func dateUTC(layout string, v any, now time.Time) string {
	var t time.Time
	switch d := v.(type) {
	case time.Time:
		t = d
	case *time.Time:
		t = *d
	case int64:
		t = time.Unix(d, 0)
	case int:
		t = time.Unix(int64(d), 0)
	case int32:
		t = time.Unix(int64(d), 0)
	default:
		t = now
	}
	return t.UTC().Format(layout)
}

func fakeBcrypt(in string) string {
	sum := sha256.Sum256([]byte("fathom-fake-bcrypt:" + in))
	enc := base64.RawStdEncoding.EncodeToString(sum[:])
	enc = strings.ReplaceAll(enc, "+", ".")
	return "$2a$10$" + (enc + enc)[:53]
}

func edKey(s *stream) (ed25519.PublicKey, ed25519.PrivateKey) {
	priv := ed25519.NewKeyFromSeed(s.bytes(ed25519.SeedSize))
	return priv.Public().(ed25519.PublicKey), priv
}

func pemKey(priv ed25519.PrivateKey) string {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func certTemplate(s *stream, now time.Time, cn string, ips, dns []any, days int, isCA bool) *x509.Certificate {
	t := &x509.Certificate{
		SerialNumber:          new(big.Int).SetBytes(s.bytes(16)),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             now.UTC().Truncate(time.Second),
		NotAfter:              now.UTC().Truncate(time.Second).Add(time.Duration(days) * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
	}
	if isCA {
		t.KeyUsage |= x509.KeyUsageCertSign
	}
	for _, ip := range ips {
		if p := net.ParseIP(fmt.Sprint(ip)); p != nil {
			t.IPAddresses = append(t.IPAddresses, p)
		}
	}
	for _, d := range dns {
		t.DNSNames = append(t.DNSNames, fmt.Sprint(d))
	}
	return t
}

func selfSigned(s *stream, now time.Time, cn string, ips, dns []any, days int, isCA bool) (Certificate, error) {
	pub, priv := edKey(s)
	t := certTemplate(s, now, cn, ips, dns, days, isCA)
	// Ed25519 signatures are deterministic, so rand.Reader is never consulted.
	der, err := x509.CreateCertificate(rand.Reader, t, t, pub, priv)
	if err != nil {
		return Certificate{}, err
	}
	return Certificate{Cert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Key: pemKey(priv)}, nil
}

func signed(s *stream, now time.Time, cn string, ips, dns []any, days int, ca Certificate) (Certificate, error) {
	caBlock, _ := pem.Decode([]byte(ca.Cert))
	keyBlock, _ := pem.Decode([]byte(ca.Key))
	if caBlock == nil || keyBlock == nil {
		return Certificate{}, fmt.Errorf("genSignedCert: ca certificate or key is not PEM")
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return Certificate{}, err
	}
	caKeyAny, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return Certificate{}, err
	}
	caKey, ok := caKeyAny.(ed25519.PrivateKey)
	if !ok {
		return Certificate{}, fmt.Errorf("genSignedCert: ca key is %T, want ed25519", caKeyAny)
	}
	pub, priv := edKey(s)
	t := certTemplate(s, now, cn, ips, dns, days, false)
	der, err := x509.CreateCertificate(rand.Reader, t, caCert, pub, caKey)
	if err != nil {
		return Certificate{}, err
	}
	return Certificate{Cert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), Key: pemKey(priv)}, nil
}
