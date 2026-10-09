package licensekey

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

func newKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv
}

func sample() License {
	return License{
		ID: "6707a1b2c3d4e5f6a7b8c9d0", Product: "dpfs", Plan: "business",
		Customer: Customer{Name: "Công ty ABC", Email: "it@abc.vn"},
		Clusters: 2,
		IssuedAt: time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2027, 10, 9, 3, 0, 0, 0, time.UTC),
		UpdatesUntil: time.Date(2027, 10, 9, 3, 0, 0, 0, time.UTC), Issuer: "store.mitekbro.com",
	}
}

func TestKeyRoundTrips(t *testing.T) {
	priv := newKey(t)
	key, err := Sign(sample(), priv)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "dpl1.") || strings.Count(key, ".") != 2 {
		t.Fatalf("key %q is not dpl1.<payload>.<signature>", key)
	}
	got, err := Parse(key, priv.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	want := sample()
	want.KeyID = KeyID(priv.Public().(ed25519.PublicKey))
	if got.ID != want.ID || got.Customer != want.Customer || got.Clusters != 2 || !got.ExpiresAt.Equal(want.ExpiresAt) || got.KeyID != want.KeyID {
		t.Fatalf("parsed %+v, want %+v", *got, want)
	}
}

func TestKeyWrappedByAMailClientStillParses(t *testing.T) {
	priv := newKey(t)
	key, _ := Sign(sample(), priv)
	wrapped := ""
	for i := 0; i < len(key); i += 60 {
		end := i + 60
		if end > len(key) {
			end = len(key)
		}
		wrapped += key[i:end] + "\r\n "
	}
	if _, err := Parse("\t"+wrapped+"\n", priv.Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("wrapped key: %v", err)
	}
}

func TestTamperingAndWrongKeysAreRefused(t *testing.T) {
	priv, other := newKey(t), newKey(t)
	key, _ := Sign(sample(), priv)
	pub := priv.Public().(ed25519.PublicKey)
	parts := strings.Split(key, ".")

	cases := map[string]string{
		"payload changed":   parts[0] + "." + parts[1][:len(parts[1])-2] + "AA." + parts[2],
		"signature changed": parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-2] + "AA",
	}
	for name, bad := range cases {
		if _, err := Parse(bad, pub); !errors.Is(err, ErrSignature) {
			t.Errorf("%s: err = %v, want ErrSignature", name, err)
		}
	}
	if _, err := Parse(key, other.Public().(ed25519.PublicKey)); !errors.Is(err, ErrSignature) {
		t.Errorf("other key: err = %v, want ErrSignature", err)
	}
	if _, err := Parse(key, other.Public().(ed25519.PublicKey), pub); err != nil {
		t.Errorf("the right key among several: %v", err)
	}
	for name, bad := range map[string]string{
		"empty": "", "prefix": "dpl2." + parts[1] + "." + parts[2], "two parts": parts[0] + "." + parts[1],
		"garbage signature": parts[0] + "." + parts[1] + ".notbase64!",
	} {
		if _, err := Parse(bad, pub); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: err = %v, want ErrFormat", name, err)
		}
	}
	if _, err := Parse(key); !errors.Is(err, ErrUnknownKey) {
		t.Errorf("no keys: err = %v, want ErrUnknownKey", err)
	}
}

func TestPayloadMustCarryTheEssentials(t *testing.T) {
	priv := newKey(t)
	l := sample()
	l.ExpiresAt = time.Time{}
	key, _ := Sign(l, priv)
	if _, err := Parse(key, priv.Public().(ed25519.PublicKey)); !errors.Is(err, ErrFormat) {
		t.Fatalf("missing expires_at: err = %v, want ErrFormat", err)
	}
}

// The same seed and payload must produce this exact key in every
// implementation (the JS SDK asserts it too), so keys issued by one can be
// read by the other.
const (
	vectorKey = "dpl1.eyJpZCI6IjY3MDdhMWIyYzNkNGU1ZjZhN2I4YzlkMCIsInByb2R1Y3QiOiJkcGZzIiwicGxhbiI6ImJ1c2luZXNzIiwiY3VzdG9tZXIiOnsibmFtZSI6IkPDtG5nIHR5IEFCQyIsImVtYWlsIjoiaXRAYWJjLnZuIn0sImNsdXN0ZXJzIjoyLCJpc3N1ZWRfYXQiOiIyMDI2LTEwLTA5VDAzOjAwOjAwWiIsImV4cGlyZXNfYXQiOiIyMDI3LTEwLTA5VDAzOjAwOjAwWiIsInVwZGF0ZXNfdW50aWwiOiIyMDI3LTEwLTA5VDAzOjAwOjAwWiIsImlzc3VlciI6InN0b3JlLm1pdGVrYnJvLmNvbSIsImtpZCI6IjM0NzUwZjk4In0.F0zG2CTLBOll3JjahBtWt6iTn-lSS9HQj4AikNSU57eUTPtlZcCjtMMuN2hJ7ZwIBg-rm2QeHYJWycrhgWCqDw"
	vectorPub = "8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c"
)

func TestCrossLanguageVector(t *testing.T) {
	priv := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	key, err := Sign(sample(), priv)
	if err != nil {
		t.Fatal(err)
	}
	if key != vectorKey {
		t.Fatalf("key changed:\n got %s\nwant %s", key, vectorKey)
	}
	pub, err := PublicKeyFromHex(vectorPub)
	if err != nil {
		t.Fatal(err)
	}
	if KeyID(pub) != "34750f98" {
		t.Fatalf("kid = %s", KeyID(pub))
	}
	l, err := Parse(vectorKey, pub)
	if err != nil || l.Customer.Name != "Công ty ABC" {
		t.Fatalf("parse vector: %+v, %v", l, err)
	}
	seeded, err := PrivateKeyFromSeedHex("0101010101010101010101010101010101010101010101010101010101010101")
	if err != nil || !seeded.Equal(priv) {
		t.Fatalf("seed hex: %v", err)
	}
}

func TestChecks(t *testing.T) {
	l := sample()
	l.ClusterIDs = []string{"aaa"}
	before, after := l.ExpiresAt.Add(-time.Hour), l.ExpiresAt.Add(time.Hour)
	if l.Expired(before) || !l.Expired(after) {
		t.Error("Expired")
	}
	if !l.UpdatesCover(before) || l.UpdatesCover(after) || !l.UpdatesCover(time.Time{}) {
		t.Error("UpdatesCover")
	}
	open := sample()
	open.UpdatesUntil = time.Time{}
	if !open.UpdatesCover(after) {
		t.Error("a license without an updates window covers every release")
	}
	if !l.AllowsCluster("aaa") || l.AllowsCluster("bbb") || !open.AllowsCluster("bbb") {
		t.Error("AllowsCluster")
	}
	if err := l.Valid(before, "aaa"); err != nil {
		t.Error(err)
	}
	if err := l.Valid(after, "aaa"); !errors.Is(err, ErrExpired) {
		t.Errorf("after expiry: %v", err)
	}
	if err := l.Valid(before, "bbb"); !errors.Is(err, ErrWrongCluster) {
		t.Errorf("wrong cluster: %v", err)
	}
}
