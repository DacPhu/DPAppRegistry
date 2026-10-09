package main

// The operator's commands for a hosted registry: accounts, their plans, and the usage
// invoices are made from. deploy/deploy.sh account|usage runs them in the backend container.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DacPhu/DPAppRegistry/mongod"
	"github.com/DacPhu/DPAppRegistry/redisdb"
	"github.com/DacPhu/DPAppRegistry/server/license"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/plans"
	"github.com/DacPhu/DPAppRegistry/server/usage"

	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const accountUsage = `Usage:
  account create <name> --plan <plan>     create an account; prints its password once
  account plan <name> <plan> [--apps N] [--storage-gb N] [--members N] [--mai N]
                                          set its plan; the limits given override the plan's
  account show <name>                     its plan, limits and usage
  account list                            every account
  account store <name> --file <profile.json>
                                          set its store page's name, texts, logo and links
  usage snapshot                          save this month's usage of every account
  usage report [--month YYYY-MM]          installs, overage and storage per account
  license …                               keys sold for the store's products (license for the list)
  order …                                 orders paid through the store (order for the list)

Plans: hobby, starter, team, business, internal.`

// Account names end up in store URLs (/@name/) and storage paths.
var accountName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}$`)

type operator struct {
	ctx    context.Context
	client *mongo.Client
	db     *mongo.Database
	rdb    *redis.Client
}

func runOperatorCommand(args []string) error {
	if len(args) < 2 && args[0] != "license" && args[0] != "order" {
		return errors.New(accountUsage)
	}
	client, configDB := mongod.ConnectToDatabase(viper.GetString("MONGODB_URL"))
	defer client.Disconnect(context.Background())
	rdb := redisdb.ConnectToRedis(redisdb.RedisConfig{
		Addr:     viper.GetString("REDIS_HOST") + ":" + viper.GetString("REDIS_PORT"),
		Password: viper.GetString("REDIS_PASSWORD"),
		DB:       viper.GetInt("REDIS_DB"),
	})
	op := operator{ctx: context.Background(), client: client, db: client.Database(configDB.Database), rdb: rdb}
	plans.Setup(op.db, rdb)
	if err := license.Setup(viper.GetString("LICENSE_SIGNING_KEY")); err != nil {
		return err
	}
	if args[0] == "license" {
		return op.runLicense(args[1:])
	}
	if args[0] == "order" {
		return op.runOrder(args[1:])
	}

	switch args[0] + " " + args[1] {
	case "account create":
		return op.create(args[2:])
	case "account plan":
		return op.setPlan(args[2:])
	case "account show":
		return op.show(args[2:])
	case "account list":
		return op.list()
	case "account store":
		return op.setStore(args[2:])
	case "usage snapshot":
		snapshots, err := usage.TakeSnapshots(op.ctx, op.db, rdb, time.Now())
		fmt.Printf("Saved this month's usage of %d accounts.\n", len(snapshots))
		return err
	case "usage report":
		return op.report(args[2:])
	}
	return errors.New(accountUsage)
}

// parse reads a command's flags wherever they sit among its arguments.
func parse(set *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := set.Parse(args); err != nil {
			return nil, err
		}
		args = set.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

func (op operator) exists(name string) (bool, error) {
	err := op.db.Collection("admins").FindOne(op.ctx, bson.M{"username": name}).Err()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	return err == nil, err
}

func (op operator) create(args []string) error {
	set := flag.NewFlagSet("account create", flag.ContinueOnError)
	planID := set.String("plan", "", "the account's plan")
	names, err := parse(set, args)
	if err != nil {
		return err
	}
	if len(names) != 1 || *planID == "" {
		return errors.New("usage: account create <name> --plan <plan>")
	}
	name := names[0]
	if !accountName.MatchString(name) {
		return fmt.Errorf("%q: use 2-39 lowercase letters, digits and dashes, starting with a letter or digit", name)
	}
	if _, ok := plans.Find(*planID); !ok {
		return fmt.Errorf("unknown plan %q", *planID)
	}
	if taken, err := op.exists(name); err != nil || taken {
		if err == nil {
			err = fmt.Errorf("an account named %s already exists", name)
		}
		return err
	}
	if err := op.db.Collection("team_users").FindOne(op.ctx, bson.M{"username": name}).Err(); err == nil {
		return fmt.Errorf("a team user named %s already exists", name)
	}

	secret := make([]byte, 18)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	password := base64.RawURLEncoding.EncodeToString(secret)
	if err := mongod.CreateUser(op.client, op.db, &model.Credentials{Username: name, Password: password}); err != nil {
		return err
	}
	if _, err := op.db.Collection("admins").UpdateOne(op.ctx, bson.M{"username": name}, bson.M{"$set": bson.M{"plan": *planID}}); err != nil {
		return err
	}
	fmt.Printf("Created %s on the %s plan.\nUsername: %s\nPassword: %s\n(shown once; they can change it under Settings)\n", name, *planID, name, password)
	if url := plans.StoreURL(name); url != "" {
		fmt.Printf("Store page: %s\n", url)
	}
	return nil
}

func (op operator) setPlan(args []string) error {
	set := flag.NewFlagSet("account plan", flag.ContinueOnError)
	apps := set.Int("apps", 0, "apps allowed")
	storageGB := set.Float64("storage-gb", 0, "storage in GB")
	members := set.Int("members", 0, "members allowed, the account included")
	mai := set.Int64("mai", 0, "monthly active installs included")
	positional, err := parse(set, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return errors.New("usage: account plan <name> <plan> [--apps N] [--storage-gb N] [--members N] [--mai N]")
	}
	name, planID := positional[0], positional[1]
	if _, ok := plans.Find(planID); !ok {
		return fmt.Errorf("unknown plan %q", planID)
	}
	if found, err := op.exists(name); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("no account named %s", name)
		}
		return err
	}
	overrides := plans.Limits{Apps: *apps, StorageBytes: int64(*storageGB * float64(int64(1)<<30)), Members: *members, MAI: *mai}
	update := bson.M{"$set": bson.M{"plan": planID}}
	if overrides == (plans.Limits{}) {
		update["$unset"] = bson.M{"plan_limits": ""}
	} else {
		update["$set"].(bson.M)["plan_limits"] = overrides
	}
	if _, err := op.db.Collection("admins").UpdateOne(op.ctx, bson.M{"username": name}, update); err != nil {
		return err
	}
	fmt.Printf("%s is on the %s plan now; the server applies it within a minute.\n", name, planID)
	return op.show([]string{name})
}

func limit(n int64, format func(int64) string) string {
	if n == 0 {
		return "unlimited"
	}
	return format(n)
}

func count(n int64) string { return fmt.Sprintf("%d", n) }

func (op operator) show(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: account show <name>")
	}
	if found, err := op.exists(args[0]); err != nil || !found {
		if err == nil {
			err = fmt.Errorf("no account named %s", args[0])
		}
		return err
	}
	account, err := plans.LoadStored(op.ctx, args[0])
	if err != nil {
		return err
	}
	overview, err := plans.OverviewOf(op.ctx, account)
	if err != nil {
		return err
	}
	u, l := overview.Usage, account.Limits
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Account\t%s\n", account.Owner)
	fmt.Fprintf(w, "Plan\t%s%s\n", account.Plan.Name, map[bool]string{true: "", false: " (not enforced: PLANS_ENABLED is off)"}[overview.Enabled])
	fmt.Fprintf(w, "Apps\t%d of %s\n", u.Apps, limit(int64(l.Apps), count))
	fmt.Fprintf(w, "Members\t%d of %s\n", u.Members, limit(int64(l.Members), count))
	fmt.Fprintf(w, "Storage\t%s of %s\n", usage.FormatBytes(u.StorageBytes), limit(l.StorageBytes, usage.FormatBytes))
	fmt.Fprintf(w, "Installs (%s)\t%d of %s (%d reported, %d estimated)\n", u.Installs.Month, u.Installs.Total(), limit(l.MAI, count), u.Installs.Measured, u.Installs.Estimated)
	for _, app := range u.Installs.Apps {
		fmt.Fprintf(w, "  %s\t%d reported, %d estimated\n", app.App, app.Measured, app.Estimated)
	}
	features := make([]string, 0, len(account.Plan.Features))
	for _, f := range account.Plan.Features {
		features = append(features, string(f))
	}
	fmt.Fprintf(w, "Features\t%s\n", strings.Join(features, ", "))
	if overview.StoreURL != "" {
		fmt.Fprintf(w, "Store page\t%s\n", overview.StoreURL)
	}
	return w.Flush()
}

func (op operator) owners() ([]string, error) {
	values, err := op.db.Collection("admins").Distinct(op.ctx, "username", bson.M{})
	if err != nil {
		return nil, err
	}
	owners := make([]string, 0, len(values))
	for _, value := range values {
		if owner, ok := value.(string); ok && owner != "" {
			owners = append(owners, owner)
		}
	}
	sort.Strings(owners)
	return owners, nil
}

func (op operator) list() error {
	owners, err := op.owners()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ACCOUNT\tPLAN\tAPPS\tSTORAGE\tINSTALLS THIS MONTH")
	for _, owner := range owners {
		account, err := plans.LoadStored(op.ctx, owner)
		if err != nil {
			return err
		}
		overview, err := plans.OverviewOf(op.ctx, account)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\n", owner, account.Plan.ID, overview.Usage.Apps, usage.FormatBytes(overview.Usage.StorageBytes), overview.Usage.Installs.Total())
	}
	return w.Flush()
}

// storeProfile is what an account may set on its store page; see store/config.js.
type storeProfile struct {
	Name    string            `json:"name,omitempty" bson:"name,omitempty"`
	Tagline map[string]string `json:"tagline,omitempty" bson:"tagline,omitempty"`
	About   map[string]string `json:"about,omitempty" bson:"about,omitempty"`
	Logo    string            `json:"logo,omitempty" bson:"logo,omitempty"`
	HomeURL string            `json:"homeUrl,omitempty" bson:"home_url,omitempty"`
	Contact string            `json:"contact,omitempty" bson:"contact,omitempty"`
	Links   []struct {
		Label map[string]string `json:"label" bson:"label"`
		URL   string            `json:"url" bson:"url"`
	} `json:"links,omitempty" bson:"links,omitempty"`
}

func (op operator) setStore(args []string) error {
	set := flag.NewFlagSet("account store", flag.ContinueOnError)
	file := set.String("file", "", "a JSON file: name, tagline, about, logo, homeUrl, contact, links")
	names, err := parse(set, args)
	if err != nil {
		return err
	}
	if len(names) != 1 || *file == "" {
		return errors.New("usage: account store <name> --file <profile.json>")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var profile storeProfile
	if err := decoder.Decode(&profile); err != nil {
		return fmt.Errorf("%s: %w", *file, err)
	}
	for _, link := range append([]string{profile.Logo, profile.HomeURL}, linkURLs(profile)...) {
		if link != "" && !strings.HasPrefix(link, "https://") {
			return fmt.Errorf("%s: links must use https://", link)
		}
	}
	result, err := op.db.Collection("admins").UpdateOne(op.ctx, bson.M{"username": names[0]}, bson.M{"$set": bson.M{"store_profile": profile}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return fmt.Errorf("no account named %s", names[0])
	}
	fmt.Printf("Saved the store page of %s: %s\n", names[0], plans.StoreURL(names[0]))
	return nil
}

func linkURLs(profile storeProfile) []string {
	urls := make([]string, 0, len(profile.Links))
	for _, link := range profile.Links {
		urls = append(urls, link.URL)
	}
	return urls
}

func (op operator) report(args []string) error {
	set := flag.NewFlagSet("usage report", flag.ContinueOnError)
	month := set.String("month", usage.Month(time.Now()), "the month, as YYYY-MM")
	if _, err := parse(set, args); err != nil {
		return err
	}
	if _, err := time.Parse("2006-01", *month); err != nil {
		return fmt.Errorf("month %q: use YYYY-MM", *month)
	}
	owners, err := op.owners()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Usage in %s\n", *month)
	fmt.Fprintln(w, "ACCOUNT\tPLAN\tINSTALLS\tINCLUDED\tOVER\tOVERAGE USD\tOVERAGE VND\tSTORAGE")
	for _, owner := range owners {
		account, err := plans.LoadStored(op.ctx, owner)
		if err != nil {
			return err
		}
		installs, err := usage.MonthlyMAI(op.ctx, op.rdb, owner, *month)
		if err != nil {
			return err
		}
		storage, err := usage.StorageBytes(op.ctx, op.db, owner)
		if err != nil {
			return err
		}
		over := int64(0)
		if account.Limits.MAI != 0 && installs.Total() > account.Limits.MAI {
			over = installs.Total() - account.Limits.MAI
		}
		// Overage is billed per started 1,000 installs.
		blocks := float64(int64(math.Ceil(float64(over) / 1000)))
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\t%.2f\t%d\t%s\n", owner, account.Plan.ID, installs.Total(), limit(account.Limits.MAI, count),
			over, blocks*account.Plan.OverageUSD, int64(blocks)*account.Plan.OverageVND, usage.FormatBytes(storage))
	}
	return w.Flush()
}
