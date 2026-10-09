package sepay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// listURL is SePay's transaction list; a test points it at a local server.
var listURL = "https://my.sepay.vn/userapi/transactions/list"

// ListedTransaction is one row of the transaction list. SePay sends its
// numbers as strings here, so they are parsed by hand.
type ListedTransaction struct {
	ID              Number `json:"id"`
	TransactionDate string `json:"transaction_date"`
	AmountIn        Number `json:"amount_in"`
	AmountOut       Number `json:"amount_out"`
	Content         string `json:"transaction_content"`
	ReferenceCode   string `json:"reference_number"`
	Gateway         string `json:"bank_brand_name"`
	AccountNumber   string `json:"account_number"`
}

// ListTransactions reads the newest limit transactions, for finding transfers
// whose webhook never arrived. It does not filter by account: SePay reports
// the main account where the store may be configured with a sub-account.
func ListTransactions(ctx context.Context, apiKey string, limit int) ([]ListedTransaction, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("sepay: SEPAY_API_KEY is not set")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL+"?limit="+strconv.Itoa(limit), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sepay: transaction list returned HTTP %d", resp.StatusCode)
	}
	var parsed struct {
		Transactions []ListedTransaction `json:"transactions"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("sepay: transaction list: %w", err)
	}
	return parsed.Transactions, nil
}

// AsTransaction turns a listed transaction into the shape a webhook delivers,
// so reconciliation takes the same path as a live webhook.
func (t ListedTransaction) AsTransaction() Transaction {
	kind := "out"
	amount := t.AmountOut
	if t.AmountIn > 0 {
		kind, amount = "in", t.AmountIn
	}
	return Transaction{
		ID: t.ID, Gateway: t.Gateway, TransactionDate: t.TransactionDate, AccountNumber: t.AccountNumber,
		Content: t.Content, TransferType: kind, TransferAmount: amount, ReferenceCode: t.ReferenceCode,
	}
}
