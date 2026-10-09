package sepay

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signed(t *testing.T, secret, body string, at time.Time) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/webhooks/sepay", strings.NewReader(body))
	ts := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + body))
	r.Header.Set("X-SePay-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	r.Header.Set("X-SePay-Timestamp", ts)
	return r
}

func TestVerifyHMAC(t *testing.T) {
	now := time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)
	cfg := Config{WebhookSecret: "s3cret"}
	body := `{"id":1,"content":"MTBABCDEFGH2"}`
	if err := Verify(signed(t, "s3cret", body, now), []byte(body), cfg, now); err != nil {
		t.Fatalf("good signature: %v", err)
	}
	if err := Verify(signed(t, "s3cret", body, now), []byte(body+" "), cfg, now); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("changed body: %v", err)
	}
	if err := Verify(signed(t, "other", body, now), []byte(body), cfg, now); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("wrong secret: %v", err)
	}
	if err := Verify(signed(t, "s3cret", body, now.Add(-7*time.Hour)), []byte(body), cfg, now); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("stale: %v", err)
	}
	if err := Verify(signed(t, "s3cret", body, now.Add(-5*time.Hour)), []byte(body), cfg, now); err != nil {
		t.Errorf("a retry five hours later is still accepted: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if err := Verify(r, nil, cfg, now); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("no headers: %v", err)
	}
}

func TestVerifyAPIKeyAndUnconfigured(t *testing.T) {
	cfg := Config{WebhookAPIKey: "k3y"}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("Authorization", "Apikey k3y")
	if err := Verify(r, nil, cfg, time.Now()); err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Apikey nope")
	if err := Verify(r, nil, cfg, time.Now()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("wrong key: %v", err)
	}
	r.Header.Set("Authorization", "Bearer k3y")
	if err := Verify(r, nil, cfg, time.Now()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("wrong scheme: %v", err)
	}
	if err := Verify(r, nil, Config{}, time.Now()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
	// With both set, the secret decides.
	if err := Verify(r, nil, Config{WebhookSecret: "s", WebhookAPIKey: "k3y"}, time.Now()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("secret takes precedence: %v", err)
	}
}

func TestPayloadNumbersAndCodes(t *testing.T) {
	var tx Transaction
	raw := `{"id":"92704","gateway":"Vietcombank","transactionDate":"2026-10-09 10:11:12","content":"chuyen tien mtbabcdefgh cam on","transferType":"in","transferAmount":"4900000.00","accumulated":19077000,"referenceCode":"MBVCB.3278907687"}`
	if err := json.Unmarshal([]byte(raw), &tx); err != nil {
		t.Fatal(err)
	}
	if tx.ID != 92704 || tx.TransferAmount != 4_900_000 || tx.Accumulated != 19_077_000 || !tx.Incoming() {
		t.Fatalf("decoded %+v", tx)
	}
	if got := ExtractCode(tx.Content); got != "MTBABCDEFGH" {
		t.Errorf("code = %q", got)
	}
	for content, want := range map[string]string{
		"MTB2345ABCD":            "MTB2345ABCD",
		"TT MTBQWERTYUP 2026":    "MTBQWERTYUP",
		"mtb-abcdefgh":           "", // a dash breaks the code
		"MTB0OI1XXXX":            "", // characters the alphabet leaves out
		"no code":                "",
		"MTBABCDEFGHJ and more":  "MTBABCDEFGH",
		"double MTBAAAAAAAA MTB": "MTBAAAAAAAA",
	} {
		if got := ExtractCode(content); got != want {
			t.Errorf("ExtractCode(%q) = %q, want %q", content, got, want)
		}
	}
	if len(PaymentCodeAlphabet) != 32 {
		t.Errorf("alphabet has %d characters", len(PaymentCodeAlphabet))
	}
}

func TestQRURL(t *testing.T) {
	cfg := Config{BankAccount: "0010000000355", BankName: "Vietcombank"}
	got := QRURL(cfg, 4_900_000, "MTBABCDEFGH2")
	want := "https://qr.sepay.vn/img?acc=0010000000355&amount=4900000&bank=Vietcombank&des=MTBABCDEFGH2&template=compact"
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if cfg.Ready() == false || (Config{}).Ready() {
		t.Error("Ready")
	}
}

func TestListTransactionsParsesStringNumbers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer api-key" || r.URL.Query().Get("limit") != "20" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"status":200,"transactions":[{"id":"1","transaction_date":"2026-10-09 09:00:00","amount_in":"4900000.00","amount_out":"0.00","transaction_content":"MTBABCDEFGH2","reference_number":"REF1","bank_brand_name":"VCB"},{"id":"2","amount_in":"0.00","amount_out":"12000.00","transaction_content":"out"}]}`))
	}))
	defer server.Close()
	listURL = server.URL
	defer func() { listURL = "https://my.sepay.vn/userapi/transactions/list" }()

	rows, err := ListTransactions(context.Background(), "api-key", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].AmountIn != 4_900_000 || rows[1].AmountOut != 12_000 {
		t.Fatalf("rows %+v", rows)
	}
	in, out := rows[0].AsTransaction(), rows[1].AsTransaction()
	if !in.Incoming() || in.TransferAmount != 4_900_000 || in.ReferenceCode != "REF1" || out.Incoming() || out.TransferAmount != 12_000 {
		t.Errorf("AsTransaction: %+v %+v", in, out)
	}
	if _, err := ListTransactions(context.Background(), "", 20); err == nil {
		t.Error("no api key must fail")
	}
}
