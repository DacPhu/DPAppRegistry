package sepay

import (
	"net/url"
	"strconv"
)

// QRURL is the VietQR image SePay renders for a transfer of amount to the
// configured account with code as its content, which the buyer's banking app
// scans.
func QRURL(cfg Config, amount int64, code string) string {
	query := url.Values{}
	query.Set("acc", cfg.BankAccount)
	query.Set("bank", cfg.BankName)
	query.Set("amount", strconv.FormatInt(amount, 10))
	query.Set("des", code)
	query.Set("template", "compact")
	return "https://qr.sepay.vn/img?" + query.Encode()
}
