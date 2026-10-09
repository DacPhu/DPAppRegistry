package main

// The operator's license commands: issue a key for a contract or a transfer
// confirmed by hand, list and inspect keys, revoke one, and print the public
// key apps are built with. deploy/deploy.sh license … runs them in the backend
// container.

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const licenseUsage = `Usage:
  license issue --product P --plan PLAN --customer "Name" [--email E] [--company C]
                [--clusters N] [--years N | --expires YYYY-MM-DD] [--cluster-ids a,b] [--note …]
                                          issue a key; printed once
  license list [--product P] [--email E]  every key, newest first
  license show <id> [--key]               one key, with the clusters that activated it
  license revoke <id> --reason "…"        mark a key revoked (apps learn of it when they activate)
  license pubkey                          the public key apps verify with, and its id

Products and plans: see server/license/catalog.go (dpfs: business, enterprise).`

func (op operator) runLicense(args []string) error {
	if len(args) == 0 {
		return errors.New(licenseUsage)
	}
	switch args[0] {
	case "issue":
		return op.issueLicense(args[1:])
	case "list":
		return op.listLicenses(args[1:])
	case "show":
		return op.showLicense(args[1:])
	case "revoke":
		return op.revokeLicense(args[1:])
	case "pubkey":
		if !license.Enabled() {
			return errors.New(license.ErrDisabled.Error())
		}
		fmt.Printf("public key: %s\nkey id:     %s\n", license.PublicKeyHex(), license.KeyID())
		return nil
	}
	return errors.New(licenseUsage)
}

func (op operator) issueLicense(args []string) error {
	set := flag.NewFlagSet("license issue", flag.ContinueOnError)
	product := set.String("product", "dpfs", "product id")
	plan := set.String("plan", "business", "plan id")
	customer := set.String("customer", "", "the customer's name")
	email := set.String("email", "", "the customer's email")
	company := set.String("company", "", "the customer's company")
	clusters := set.Int("clusters", 1, "servers the key covers; 0 for no limit")
	years := set.Int("years", 1, "term in years from today")
	expires := set.String("expires", "", "term end as YYYY-MM-DD (overrides --years)")
	clusterIDs := set.String("cluster-ids", "", "lock the key to these cluster ids, comma-separated")
	note := set.String("note", "", "a note for the operator")
	if _, err := parse(set, args); err != nil {
		return err
	}
	if *customer == "" {
		return errors.New("--customer is required")
	}
	end := time.Now().AddDate(*years, 0, 0)
	if *expires != "" {
		parsed, err := time.Parse("2006-01-02", *expires)
		if err != nil {
			return fmt.Errorf("--expires %q: use YYYY-MM-DD", *expires)
		}
		end = parsed.Add(24*time.Hour - time.Second)
	}
	var ids []string
	for _, id := range strings.Split(*clusterIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	doc, err := license.Issue(op.ctx, op.db, license.IssueRequest{
		Owner: op.owner(), Product: *product, Plan: *plan,
		Customer: model.LicenseCustomer{Name: *customer, Email: *email, Company: *company},
		Clusters: *clusters, ClusterIDs: ids, ExpiresAt: end, IssuedBy: "operator", Note: *note,
	})
	if err != nil {
		return err
	}
	fmt.Printf("Issued %s %s for %s, %d cluster(s), until %s.\nLicense id: %s\n\n%s\n\n(the key is shown once; license show --key prints it again)\n",
		doc.Product, doc.Plan, doc.Customer.Name, doc.Clusters, doc.ExpiresAt.Format("2006-01-02"), doc.ID.Hex(), doc.Key)
	return nil
}

// owner is the account licenses are issued under: the store's owner.
func (op operator) owner() string {
	if owner := os.Getenv("STORE_OWNER"); owner != "" {
		return owner
	}
	return "admin"
}

func (op operator) listLicenses(args []string) error {
	set := flag.NewFlagSet("license list", flag.ContinueOnError)
	product := set.String("product", "", "only this product")
	email := set.String("email", "", "only this customer email")
	if _, err := parse(set, args); err != nil {
		return err
	}
	filter := bson.M{}
	if *product != "" {
		filter["product"] = *product
	}
	if *email != "" {
		filter["customer.email"] = *email
	}
	cursor, err := op.db.Collection("licenses").Find(op.ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return err
	}
	defer cursor.Close(op.ctx)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPRODUCT\tPLAN\tCUSTOMER\tCLUSTERS\tEXPIRES\tSTATE\tISSUED BY")
	for cursor.Next(op.ctx) {
		var doc model.License
		if err := cursor.Decode(&doc); err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n", doc.ID.Hex(), doc.Product, doc.Plan, doc.Customer.Name, doc.Clusters,
			doc.ExpiresAt.Format("2006-01-02"), licenseState(doc), doc.IssuedBy)
	}
	return w.Flush()
}

func licenseState(doc model.License) string {
	switch {
	case doc.RevokedAt != nil:
		return "revoked"
	case time.Now().After(doc.ExpiresAt):
		return "expired"
	}
	return "active"
}

func (op operator) showLicense(args []string) error {
	set := flag.NewFlagSet("license show", flag.ContinueOnError)
	withKey := set.Bool("key", false, "print the key")
	ids, err := parse(set, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 {
		return errors.New("usage: license show <id> [--key]")
	}
	id, err := primitive.ObjectIDFromHex(ids[0])
	if err != nil {
		return fmt.Errorf("%q is not a license id", ids[0])
	}
	var doc model.License
	if err := op.db.Collection("licenses").FindOne(op.ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return fmt.Errorf("no license %s", ids[0])
	}
	activations, err := license.Activations(op.ctx, op.db, id)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "License\t%s (%s)\n", doc.ID.Hex(), licenseState(doc))
	fmt.Fprintf(w, "Product\t%s, %s plan\n", doc.Product, doc.Plan)
	fmt.Fprintf(w, "Customer\t%s", doc.Customer.Name)
	if doc.Customer.Company != "" {
		fmt.Fprintf(w, ", %s", doc.Customer.Company)
	}
	if doc.Customer.Email != "" {
		fmt.Fprintf(w, " <%s>", doc.Customer.Email)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Clusters\t%s\n", limit(int64(doc.Clusters), count))
	if len(doc.ClusterIDs) > 0 {
		fmt.Fprintf(w, "Locked to\t%s\n", strings.Join(doc.ClusterIDs, ", "))
	}
	fmt.Fprintf(w, "Issued\t%s by %s\n", doc.IssuedAt.Format("2006-01-02"), doc.IssuedBy)
	fmt.Fprintf(w, "Expires\t%s (updates until %s)\n", doc.ExpiresAt.Format("2006-01-02"), doc.UpdatesUntil.Format("2006-01-02"))
	if doc.RevokedAt != nil {
		fmt.Fprintf(w, "Revoked\t%s: %s\n", doc.RevokedAt.Format("2006-01-02"), doc.RevokeReason)
	}
	if doc.OrderID != nil {
		fmt.Fprintf(w, "Order\t%s\n", doc.OrderID.Hex())
	}
	if doc.Note != "" {
		fmt.Fprintf(w, "Note\t%s\n", doc.Note)
	}
	fmt.Fprintf(w, "Activations\t%d\n", len(activations))
	for _, a := range activations {
		fmt.Fprintf(w, "  %s\t%s %s/%s, first %s, last %s, %d checks\n", a.ClusterID, a.Version, a.Platform, a.Arch,
			a.FirstSeen.Format("2006-01-02"), a.LastSeen.Format("2006-01-02"), a.Count)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if *withKey {
		fmt.Printf("\n%s\n", doc.Key)
	}
	return nil
}

func (op operator) revokeLicense(args []string) error {
	set := flag.NewFlagSet("license revoke", flag.ContinueOnError)
	reason := set.String("reason", "", "why")
	ids, err := parse(set, args)
	if err != nil {
		return err
	}
	if len(ids) != 1 || *reason == "" {
		return errors.New("usage: license revoke <id> --reason \"…\"")
	}
	id, err := primitive.ObjectIDFromHex(ids[0])
	if err != nil {
		return fmt.Errorf("%q is not a license id", ids[0])
	}
	var doc model.License
	if err := op.db.Collection("licenses").FindOne(op.ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return fmt.Errorf("no license %s", ids[0])
	}
	if err := license.Revoke(op.ctx, op.db, doc.Owner, id, *reason); err != nil {
		return err
	}
	fmt.Printf("Revoked %s (%s, %s).\n", id.Hex(), doc.Customer.Name, doc.Plan)
	return nil
}
