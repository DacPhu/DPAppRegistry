package license

import (
	"context"
	"errors"
	"fmt"

	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/sepay"

	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/mongo"
)

// Results of handling one bank transaction.
const (
	ResultDuplicate   = "duplicate"    // this transaction was handled before
	ResultIgnoredOut  = "ignored_out"  // money going out
	ResultNoCode      = "no_code"      // no payment code in the content
	ResultUnknownCode = "unknown_code" // a code no order has
	ResultUnderpaid   = "underpaid"    // less than the order's amount
	ResultMatched     = "matched"      // the order is paid and its license issued
)

// Outcome is what a transaction led to.
type Outcome struct {
	Result  string
	Code    string
	Order   *model.Order
	License *model.License
}

// ProcessTransaction matches one incoming transfer to its order and fulfils
// it. With dryRun it only reports what it would do. A webhook and a
// reconciliation of the transaction list both come through here, so a
// transaction is acted on once whichever way it arrives.
func ProcessTransaction(ctx context.Context, db *mongo.Database, tx sepay.Transaction, dryRun bool) (Outcome, error) {
	outcome := Outcome{Code: sepay.ExtractCode(tx.Content)}
	eventID := fmt.Sprintf("sepay_%d", int64(tx.ID))
	if !dryRun {
		fresh, err := RecordWebhookEvent(ctx, db, model.WebhookEvent{EventID: eventID, Provider: "sepay", Code: outcome.Code, Amount: int64(tx.TransferAmount)})
		if err != nil {
			return outcome, err
		}
		if !fresh {
			outcome.Result = ResultDuplicate
			return outcome, nil
		}
	}
	finish := func(result string, orderID ...*model.Order) {
		outcome.Result = result
		if dryRun {
			return
		}
		if len(orderID) > 0 && orderID[0] != nil {
			FinishWebhookEvent(ctx, db, eventID, result, &orderID[0].ID)
		} else {
			FinishWebhookEvent(ctx, db, eventID, result, nil)
		}
	}

	if !tx.Incoming() {
		finish(ResultIgnoredOut)
		return outcome, nil
	}
	if outcome.Code == "" {
		finish(ResultNoCode)
		return outcome, nil
	}
	order, err := FindOrderByCode(ctx, db, outcome.Code)
	if errors.Is(err, ErrOrderNotFound) {
		finish(ResultUnknownCode)
		if !dryRun {
			NotifyAnomaly(fmt.Sprintf("SePay: a transfer of %d ₫ carries payment code %s, which no order has (transaction %d, %s).", int64(tx.TransferAmount), outcome.Code, int64(tx.ID), tx.Gateway))
		}
		return outcome, nil
	}
	if err != nil {
		return outcome, err
	}
	outcome.Order = order
	if int64(tx.TransferAmount) < order.AmountVND {
		finish(ResultUnderpaid, order)
		if !dryRun {
			NotifyAnomaly(fmt.Sprintf("SePay: order %s (%s) was paid %d ₫ of %d ₫ by %s; nothing was issued.", order.ID.Hex(), outcome.Code, int64(tx.TransferAmount), order.AmountVND, order.Customer.Email))
		}
		return outcome, nil
	}
	if int64(tx.TransferAmount) > order.AmountVND {
		logrus.Infof("Order %s was overpaid: %d of %d", order.ID.Hex(), int64(tx.TransferAmount), order.AmountVND)
	}
	if dryRun {
		outcome.Result = ResultMatched
		return outcome, nil
	}

	payment := &model.OrderPayment{Provider: "sepay", TransactionID: int64(tx.ID), ReferenceCode: tx.ReferenceCode,
		TransactionDate: tx.TransactionDate, Gateway: tx.Gateway, Amount: int64(tx.TransferAmount)}
	paid, lic, err := Fulfil(ctx, db, order.ID, payment)
	if err != nil {
		FinishWebhookEvent(ctx, db, eventID, "failed", &order.ID)
		return outcome, err
	}
	outcome.Order, outcome.License = paid, lic
	finish(ResultMatched, paid)
	AfterPaid(db, paid, lic)
	return outcome, nil
}
