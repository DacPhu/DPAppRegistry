package mail

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestDisabledWithoutAHost(t *testing.T) {
	viper.Set("SMTP_HOST", "")
	viper.Set("SMTP_FROM", "Store <store@example.test>")
	if Enabled() {
		t.Fatal("enabled without SMTP_HOST")
	}
	if err := Send(context.Background(), "a@b.test", "s", "b"); err == nil {
		t.Fatal("Send without configuration must fail")
	}
}

func TestMessageEncodesTheSubjectAndEndsLines(t *testing.T) {
	msg := string(Message("Mitekbro Store <store@mitekbro.com>", "it@abc.vn", "License DPFileSystem của bạn", "Xin chào,\nkey ở đây.\n"))
	for _, want := range []string{
		"From: Mitekbro Store <store@mitekbro.com>\r\n", "To: it@abc.vn\r\n",
		"Subject: =?utf-8?q?License_DPFileSystem_c=E1=BB=A7a_b=E1=BA=A1n?=\r\n",
		"Content-Type: text/plain; charset=utf-8\r\n", "\r\n\r\nXin chào,\r\nkey ở đây.\r\n", "@mitekbro.com>\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
	if bareAddress("Store <store@example.test>") != "store@example.test" || bareAddress("x@y.test") != "x@y.test" {
		t.Error("bareAddress")
	}
}
