package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"fmt"
	htmltmpl "html/template"
	"mime"
	"net"
	"net/smtp"
	"strings"
	texttmpl "text/template"
	"time"

	"github.com/sonicore/server/internal/config"
	"github.com/sonicore/server/internal/core/domain"
)

//go:embed templates/*.html templates/*.txt
var templateFS embed.FS

var htmlTemplates = htmltmpl.Must(htmltmpl.ParseFS(templateFS, "templates/*.html"))
var textTemplates = texttmpl.Must(texttmpl.ParseFS(templateFS, "templates/*.txt"))

type Sender struct {
	cfg config.EmailConfig
}

func NewSender(cfg config.EmailConfig) *Sender {
	return &Sender{cfg: cfg}
}

func (s *Sender) ChannelType() domain.ChannelType { return domain.ChannelEmail }

func (s *Sender) Name() string { return "Email" }

func (s *Sender) Enabled() bool { return s.cfg.Enabled && s.cfg.SMTPHost != "" }

func (s *Sender) Send(ctx context.Context, msg *domain.NotificationMessage) error {
	return s.SendWithConfig(ctx, msg, s.cfg)
}

func (s *Sender) SendWithConfig(ctx context.Context, msg *domain.NotificationMessage, cfg config.EmailConfig) error {
	if len(msg.To) == 0 {
		return nil
	}

	textBody, htmlBody, err := s.render(msg)
	if err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.SMTPHost)

	body := buildMIMEMessage(msg.To, cfg.FromAddress, cfg.FromName, msg.Subject, textBody, htmlBody)

	var client *smtp.Client
	if cfg.TLS && cfg.SMTPPort == 465 {
		client, err = s.dialSSL(ctx, addr, cfg)
	} else {
		client, err = s.dial(ctx, addr, cfg)
	}
	if err != nil {
		return err
	}
	defer client.Quit()

	if cfg.TLS && cfg.SMTPPort != 465 {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return fmt.Errorf("STARTTLS not supported by server (config requires TLS)")
		}
		tlsCfg := &tls.Config{ServerName: cfg.SMTPHost}
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	return sendWithClient(client, auth, cfg.FromAddress, msg.To, body)
}

func (s *Sender) dialSSL(ctx context.Context, addr string, cfg config.EmailConfig) (*smtp.Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tls dial: %w", err)
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: cfg.SMTPHost})
	client, err := smtp.NewClient(tlsConn, cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp client: %w", err)
	}
	return client, nil
}

func (s *Sender) dial(ctx context.Context, addr string, cfg config.EmailConfig) (*smtp.Client, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	client, err := smtp.NewClient(conn, cfg.SMTPHost)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp client: %w", err)
	}
	return client, nil
}

func sendWithClient(client *smtp.Client, auth smtp.Auth, from string, to []string, body []byte) error {
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt %s: %w", rcpt, err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err = w.Write(body); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return w.Close()
}

func (s *Sender) render(msg *domain.NotificationMessage) (textBody, htmlBody string, err error) {
	tmplName := string(msg.Type)
	if msg.TextBody != "" && msg.HTMLBody != "" {
		return msg.TextBody, msg.HTMLBody, nil
	}

	data := make(map[string]any, len(msg.Metadata)+1)
	for k, v := range msg.Metadata {
		data[k] = v
	}
	data["Timestamp"] = time.Now().Format(time.RFC1123)

	if msg.TextBody == "" {
		t := textTemplates.Lookup(tmplName + ".txt")
		if t != nil {
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return "", "", err
			}
			textBody = buf.String()
		} else {
			textBody = msg.Subject
		}
	} else {
		textBody = msg.TextBody
	}

	if msg.HTMLBody == "" {
		t := htmlTemplates.Lookup(tmplName + ".html")
		if t != nil {
			var buf bytes.Buffer
			if err := t.Execute(&buf, data); err != nil {
				return "", "", err
			}
			htmlBody = buf.String()
		}
	} else {
		htmlBody = msg.HTMLBody
	}

	return textBody, htmlBody, nil
}

func sanitizeHeader(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", "")
}

func formatAddress(addr, name string) string {
	if name == "" {
		return addr
	}
	return mime.QEncoding.Encode("utf-8", name) + " <" + addr + ">"
}

func base64Wrap(src []byte) []byte {
	encoded := base64.StdEncoding.EncodeToString(src)
	var buf strings.Builder
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		buf.WriteString(encoded[i:end])
		buf.WriteString("\r\n")
	}
	return []byte(buf.String())
}

func messageID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x%x", time.Now().UnixNano(), time.Now().UnixMilli())
	}
	return fmt.Sprintf("%x", b)
}

func buildMIMEMessage(to []string, fromAddr, fromName, subject, textBody, htmlBody string) []byte {
	var b strings.Builder
	from := formatAddress(fromAddr, fromName)
	b.WriteString(fmt.Sprintf("From: %s\r\n", sanitizeHeader(from)))
	b.WriteString(fmt.Sprintf("To: %s\r\n", sanitizeHeader(strings.Join(to, ", "))))
	b.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", sanitizeHeader(subject))))
	b.WriteString(fmt.Sprintf("Message-ID: <%s@sonicore>\r\n", messageID()))
	b.WriteString("MIME-Version: 1.0\r\n")

	if htmlBody != "" {
		boundary := "sonicore-boundary"
		b.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n", boundary))
		b.WriteString("\r\n")
		b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("\r\n")
		b.Write(base64Wrap([]byte(textBody)))
		b.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		b.WriteString("Content-Type: text/html; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("\r\n")
		b.Write(base64Wrap([]byte(htmlBody)))
		b.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		b.WriteString("\r\n")
		b.Write(base64Wrap([]byte(textBody)))
	}

	return []byte(b.String())
}
