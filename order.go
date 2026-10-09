package main

// The operator's order commands: what the store sold, finishing an order by
// hand (a transfer confirmed outside SePay, or a webhook that failed), and
// reconciling SePay's transaction list against pending orders.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/sepay"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const orderUsage = `Usage:
  order list [--status pending|paid|cancelled]   orders, newest first
  order show <id>                                 one order, with its page link and license
  order fulfil <id> [--amount N] [--note …]       mark an order paid by hand and issue its license
  order reconcile [--dry-run] [--limit N]         match SePay's recent transactions to pending orders`

func (op operator) runOrder(args []string) error {
	if len(args) == 0 {
		return errors.New(orderUsage)
	}
	switch args[0] {
	case "list":
		return op.listOrders(args[1:])
	case "show":
		return op.showOrder(args[1:])
	case "fulfil", "fulfill":
		return op.fulfilOrder(args[1:])
	case "reconcile":
		return op.reconcileOrders(args[1:])
	}
	return errors.New(orderUsage)
}

func (op operator) listOrders(args []string) error {
	set := flag.NewFlagSet("order list", flag.ContinueOnError)
	status := set.String("status", "", "only this status")
	if _, err := parse(set, args); err != nil {
		return err
	}
	filter := bson.M{}
	if *status != "" {
		filter["status"] = *status
	}
	cursor, err := op.db.Collection("orders").Find(op.ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(200))
	if err != nil {
		return err
	}
	defer cursor.Close(op.ctx)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCREATED\tPRODUCT\tPLAN\tQTY\tAMOUNT\tCODE\tSTATUS\tCUSTOMER\tLICENSE")
	now := time.Now()
	for cursor.Next(op.ctx) {
		var o model.Order
		if err := cursor.Decode(&o); err != nil {
			return err
		}
		lic := "-"
		if o.LicenseID != nil {
			lic = o.LicenseID.Hex()
		} else if o.Status == license.OrderPaid {
			lic = "MISSING: order fulfil " + o.ID.Hex()
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", o.ID.Hex(), o.CreatedAt.Local().Format("2006-01-02 15:04"), o.Product, o.Plan, o.Quantity,
			license.VND(o.AmountVND), o.PaymentCode, license.DisplayStatus(&o, now), o.Customer.Email, lic)
	}
	return w.Flush()
}

func (op operator) loadOrder(arg string) (*model.Order, error) {
	id, err := primitive.ObjectIDFromHex(arg)
	if err != nil {
		return nil, fmt.Errorf("%q is not an order id", arg)
	}
	return license.GetOrder(op.ctx, op.db, id)
}

func (op operator) showOrder(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: order show <id>")
	}
	o, err := op.loadOrder(args[0])
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Order\t%s (%s)\n", o.ID.Hex(), license.DisplayStatus(o, time.Now()))
	fmt.Fprintf(w, "Product\t%s, %s plan × %d = %s\n", o.Product, o.Plan, o.Quantity, license.VND(o.AmountVND))
	fmt.Fprintf(w, "Payment code\t%s\n", o.PaymentCode)
	fmt.Fprintf(w, "Customer\t%s <%s> %s\n", o.Customer.Name, o.Customer.Email, o.Customer.Company)
	fmt.Fprintf(w, "Created\t%s, expires %s\n", o.CreatedAt.Local().Format("2006-01-02 15:04"), o.ExpiresAt.Local().Format("2006-01-02 15:04"))
	if o.Payment != nil {
		fmt.Fprintf(w, "Paid\t%s via %s, transaction %d, %s\n", license.VND(o.Payment.Amount), o.Payment.Provider, o.Payment.TransactionID, o.Payment.ReferenceCode)
	}
	if o.LicenseID != nil {
		fmt.Fprintf(w, "License\t%s (license show %s --key)\n", o.LicenseID.Hex(), o.LicenseID.Hex())
	}
	fmt.Fprintf(w, "Email sent\t%v\n", o.EmailSent)
	fmt.Fprintf(w, "Page\t%s\n", license.OrderURL(o))
	return w.Flush()
}

func (op operator) fulfilOrder(args []string) error {
	set := flag.NewFlagSet("order fulfil", flag.ContinueOnError)
	amount := set.Int64("amount", 0, "what was paid; defaults to the order's amount")
	note := set.String("note", "confirmed by hand", "how the payment was confirmed")
	ids, err := parse(set, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return errors.New("usage: order fulfil <id> [--amount N] [--note …]")
	}
	o, err := op.loadOrder(ids[0])
	if err != nil {
		return err
	}
	if *amount == 0 {
		*amount = o.AmountVND
	}
	var payment *model.OrderPayment
	if o.Payment == nil {
		payment = &model.OrderPayment{Provider: "manual", Amount: *amount, ReferenceCode: *note, TransactionDate: time.Now().Format(time.RFC3339)}
	}
	paid, lic, err := license.Fulfil(op.ctx, op.db, o.ID, payment)
	if err != nil {
		return err
	}
	license.AfterPaid(op.db, paid, lic)
	time.Sleep(2 * time.Second) // let the notifications go out before the process ends
	fmt.Printf("Order %s is paid; license %s issued to %s.\n%s\n", paid.ID.Hex(), lic.ID.Hex(), paid.Customer.Email, license.OrderURL(paid))
	return nil
}

func (op operator) reconcileOrders(args []string) error {
	set := flag.NewFlagSet("order reconcile", flag.ContinueOnError)
	dryRun := set.Bool("dry-run", false, "only say what would happen")
	limit := set.Int("limit", 50, "how many recent transactions to read")
	if _, err := parse(set, args); err != nil {
		return err
	}
	rows, err := sepay.ListTransactions(op.ctx, sepay.FromViper().APIKey, *limit)
	if err != nil {
		return err
	}
	fmt.Printf("%d transactions from SePay\n", len(rows))
	matched := 0
	for _, row := range rows {
		tx := row.AsTransaction()
		if !tx.Incoming() {
			continue
		}
		outcome, err := license.ProcessTransaction(op.ctx, op.db, tx, *dryRun)
		if err != nil {
			fmt.Printf("  %d  %s  %s: ERROR %v\n", int64(tx.ID), license.VND(int64(tx.TransferAmount)), outcome.Code, err)
			continue
		}
		if outcome.Result == license.ResultDuplicate || (outcome.Result == license.ResultNoCode && outcome.Code == "") {
			continue
		}
		line := fmt.Sprintf("  %d  %s  %s: %s", int64(tx.ID), license.VND(int64(tx.TransferAmount)), outcome.Code, outcome.Result)
		if outcome.Order != nil {
			line += " order " + outcome.Order.ID.Hex()
		}
		if outcome.License != nil {
			line += " license " + outcome.License.ID.Hex()
			matched++
		}
		fmt.Println(line)
	}
	if *dryRun {
		fmt.Println("Dry run: nothing was changed.")
	} else if matched > 0 {
		time.Sleep(2 * time.Second) // notifications
		fmt.Printf("Fulfilled %d order(s).\n", matched)
	}
	return nil
}
