package sepay

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// Number reads a JSON number whether SePay sends it as 10000, 10000.00 or
// "10000.00"; the transaction list uses strings, the webhook numbers.
type Number int64

func (n *Number) UnmarshalJSON(data []byte) error {
	data = bytes.Trim(bytes.TrimSpace(data), `"`)
	if len(data) == 0 || string(data) == "null" {
		*n = 0
		return nil
	}
	value, err := strconv.ParseFloat(string(data), 64)
	if err != nil {
		return err
	}
	*n = Number(value)
	return nil
}

// Transaction is what SePay posts for one bank transaction.
type Transaction struct {
	ID              Number `json:"id"`
	Gateway         string `json:"gateway"`
	TransactionDate string `json:"transactionDate"`
	AccountNumber   string `json:"accountNumber"`
	SubAccount      string `json:"subAccount"`
	Code            string `json:"code"`
	Content         string `json:"content"`
	TransferType    string `json:"transferType"`
	Description     string `json:"description"`
	TransferAmount  Number `json:"transferAmount"`
	Accumulated     Number `json:"accumulated"`
	ReferenceCode   string `json:"referenceCode"`
}

// Incoming reports whether money came in.
func (t Transaction) Incoming() bool {
	return strings.EqualFold(t.TransferType, "in")
}

// PaymentCodePrefix opens every payment code the store makes.
const PaymentCodePrefix = "MTB"

// PaymentCodeAlphabet leaves out the characters people misread: 0/O, 1/I.
const PaymentCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

var paymentCodePattern = regexp.MustCompile(PaymentCodePrefix + "[" + PaymentCodeAlphabet + "]{8}")

// ExtractCode finds the store's payment code in a transfer's content, however
// the bank or the buyer cased or spaced the rest of it.
func ExtractCode(content string) string {
	return paymentCodePattern.FindString(strings.ToUpper(content))
}
