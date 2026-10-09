// Package mail sends the registry's few emails (a buyer's license) over SMTP.
// It is off until SMTP_HOST is set; SMTP_PORT (587, or 465 for implicit TLS),
// SMTP_USER, SMTP_PASS and SMTP_FROM complete the setup.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Enabled reports whether mail can be sent.
func Enabled() bool {
	return viper.GetString("SMTP_HOST") != "" && viper.GetString("SMTP_FROM") != ""
}

type settings struct {
	host, port, user, pass, from string
}

func current() settings {
	port := viper.GetString("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	return settings{host: viper.GetString("SMTP_HOST"), port: port, user: viper.GetString("SMTP_USER"), pass: viper.GetString("SMTP_PASS"), from: viper.GetString("SMTP_FROM")}
}

// Message builds an RFC 5322 text message with a UTF-8 subject.
func Message(from, to, subject, body string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%d.%s>\r\n", time.Now().UnixNano(), addressDomain(from))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String())
}

func addressDomain(from string) string {
	address := from
	if start := strings.LastIndex(from, "<"); start >= 0 {
		address = strings.TrimSuffix(from[start+1:], ">")
	}
	if at := strings.LastIndex(address, "@"); at >= 0 {
		return address[at+1:]
	}
	return "localhost"
}

func bareAddress(from string) string {
	if start := strings.LastIndex(from, "<"); start >= 0 {
		return strings.TrimSuffix(from[start+1:], ">")
	}
	return strings.TrimSpace(from)
}

// Send delivers one plain-text message.
func Send(ctx context.Context, to, subject, body string) error {
	if !Enabled() {
		return errors.New("mail: SMTP is not configured")
	}
	s := current()
	address := net.JoinHostPort(s.host, s.port)
	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var client *smtp.Client
	if s.port == "465" {
		conn, err := tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: s.host})
		if err != nil {
			return err
		}
		if client, err = smtp.NewClient(conn, s.host); err != nil {
			return err
		}
	} else {
		conn, err := dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return err
		}
		if client, err = smtp.NewClient(conn, s.host); err != nil {
			return err
		}
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
				return err
			}
		}
	}
	defer client.Close()

	if s.user != "" {
		if err := client.Auth(smtp.PlainAuth("", s.user, s.pass, s.host)); err != nil {
			return err
		}
	}
	if err := client.Mail(bareAddress(s.from)); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(Message(s.from, to, subject, body)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
