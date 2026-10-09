package license

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DacPhu/DPAppRegistry/server/mail"
	"github.com/DacPhu/DPAppRegistry/server/model"
	"github.com/DacPhu/DPAppRegistry/server/utils"

	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// OrderURL is the page where the buyer sees the order and, once paid, the key.
func OrderURL(order *model.Order) string {
	base := strings.TrimRight(viper.GetString("STORE_URL"), "/")
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/order/%s?t=%s", base, order.ID.Hex(), order.ViewToken)
}

// VND writes an amount the Vietnamese way: 4.900.000 ₫.
func VND(amount int64) string {
	return message.NewPrinter(language.Vietnamese).Sprintf("%d ₫", amount)
}

// AfterPaid tells the buyer and the operator about a fulfilled order, in the
// background: the webhook has already been answered.
func AfterPaid(db *mongo.Database, order *model.Order, lic *model.License) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		sent := false
		if mail.Enabled() && order.Customer.Email != "" {
			if err := mail.Send(ctx, order.Customer.Email, licenseMailSubject(order, lic), licenseMailBody(order, lic)); err != nil {
				logrus.Errorf("Order %s: sending the license email failed: %v", order.ID.Hex(), err)
			} else {
				sent = true
				_, _ = db.Collection("orders").UpdateOne(ctx, bson.M{"_id": order.ID}, bson.M{"$set": bson.M{"email_sent": true}})
			}
		}
		utils.SendSlackText(fmt.Sprintf("Order %s paid: %s %s × %d, %s, %s — license %s, email %s.",
			order.ID.Hex(), order.Product, order.Plan, order.Quantity, VND(order.AmountVND), order.Customer.Email, lic.ID.Hex(), map[bool]string{true: "sent", false: "not sent"}[sent]))
	}()
}

// NotifyAnomaly tells the operator about a transfer that needs a look.
func NotifyAnomaly(text string) {
	go utils.SendSlackText(text)
}

func licenseMailSubject(order *model.Order, lic *model.License) string {
	product, _, _ := Find(order.Product, order.Plan)
	name := product.Name
	if name == "" {
		name = order.Product
	}
	if order.Lang == "en" {
		return fmt.Sprintf("Your %s license", name)
	}
	return fmt.Sprintf("License %s của bạn", name)
}

func licenseMailBody(order *model.Order, lic *model.License) string {
	product, plan, _ := Find(order.Product, order.Plan)
	name := product.Name
	if name == "" {
		name = order.Product
	}
	until := lic.ExpiresAt.Format("02/01/2006")
	vi := fmt.Sprintf(`Cảm ơn bạn đã mua %s (gói %s).

Mã đơn hàng: %s
Mã thanh toán: %s
Số tiền: %s
Số máy chủ: %d
Hiệu lực và cập nhật đến: %s

License key (dán nguyên một dòng):

%s

Cài đặt trên máy chủ:
  dpfs license install '%s'

Xem lại đơn hàng và key bất cứ lúc nào: %s

Cần hỗ trợ: %s
`, name, plan.Name, order.ID.Hex(), order.PaymentCode, VND(order.AmountVND), order.Quantity, until, lic.Key, lic.Key, OrderURL(order), viper.GetString("PLANS_CONTACT"))
	en := fmt.Sprintf(`Thank you for buying %s (%s plan).

Order: %s
Payment code: %s
Amount: %s
Servers: %d
Valid, with updates, until: %s

License key (paste it as one line):

%s

Install it on the server:
  dpfs license install '%s'

See the order and the key again any time: %s

Support: %s
`, name, plan.Name, order.ID.Hex(), order.PaymentCode, VND(order.AmountVND), order.Quantity, lic.ExpiresAt.Format("2006-01-02"), lic.Key, lic.Key, OrderURL(order), viper.GetString("PLANS_CONTACT"))
	if order.Lang == "en" {
		return en + "\n----\n\n" + vi
	}
	return vi + "\n----\n\n" + en
}
