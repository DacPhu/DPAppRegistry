// Package sepay talks to SePay (sepay.vn), which watches a Vietnamese bank
// account and posts each incoming transfer to a webhook. The store sells
// licenses with it: the buyer transfers the amount with a payment code in the
// transfer content, SePay reports the transfer, the registry matches the code.
package sepay

import "github.com/spf13/viper"

// Config is read from the registry's environment.
type Config struct {
	// The receiving account, shown to the buyer and encoded in the QR image.
	BankAccount   string
	BankName      string
	AccountHolder string
	// WebhookSecret verifies webhooks by HMAC-SHA256 (preferred); WebhookAPIKey
	// by the Apikey header. One of them must be set for webhooks to be accepted.
	WebhookSecret string
	WebhookAPIKey string
	// APIKey reads the transaction list, for reconciling missed webhooks.
	APIKey string
}

// FromViper reads the SEPAY_* settings.
func FromViper() Config {
	return Config{
		BankAccount:   viper.GetString("SEPAY_BANK_ACCOUNT"),
		BankName:      viper.GetString("SEPAY_BANK_NAME"),
		AccountHolder: viper.GetString("SEPAY_ACCOUNT_HOLDER"),
		WebhookSecret: viper.GetString("SEPAY_WEBHOOK_SECRET"),
		WebhookAPIKey: viper.GetString("SEPAY_WEBHOOK_APIKEY"),
		APIKey:        viper.GetString("SEPAY_API_KEY"),
	}
}

// Ready reports whether buyers can be told where to pay.
func (c Config) Ready() bool {
	return c.BankAccount != "" && c.BankName != ""
}

// WebhookConfigured reports whether webhooks can be verified.
func (c Config) WebhookConfigured() bool {
	return c.WebhookSecret != "" || c.WebhookAPIKey != ""
}
