// Package email provides a transactional email provider interface with SMTP, Log, and Noop implementations.
// Follows the same pattern as push.Provider.
//
// Usage:
//
//	provider, _ := email.NewSMTPProvider(email.SMTPConfig{
//	    Host: "smtp.gmail.com", Port: 587,
//	    Username: "app@example.com", Password: "secret",
//	    From: "app@example.com", FromName: "My App",
//	})
//	provider.Send("user@example.com", "Welcome!", "Thanks for signing up.", "")
//
//	// Or with templates:
//	renderer := email.NewRenderer()
//	renderer.Register("welcome", email.Template{
//	    Subject: "Welcome to {{.AppName}}",
//	    HTML:    "<h1>Hi {{.Name}}</h1><p>Welcome!</p>",
//	    Text:    "Hi {{.Name}}, welcome!",
//	})
//	subj, html, text, _ := renderer.Render("welcome", map[string]any{"AppName": "MyApp", "Name": "Paco"})
//	provider.Send("user@example.com", subj, text, html)
package email

import (
	"bytes"
	"fmt"
	htmltmpl "html/template"
	"log"
	"net/smtp"
	"strings"
	texttmpl "text/template"
)

// Provider is the interface for sending emails.
type Provider interface {
	// Send sends an email. htmlBody is optional — if empty, sends plain text only.
	Send(to, subject, textBody, htmlBody string) error
}

// ── LogProvider ────────────────────────────────────────────────────────────

// LogProvider logs emails instead of sending them. Useful for development.
type LogProvider struct{}

func (p *LogProvider) Send(to, subject, textBody, htmlBody string) error {
	log.Printf("[email/log] to=%s subject=%q body=%s", to, subject, truncate(textBody, 80))
	return nil
}

// ── NoopProvider ───────────────────────────────────────────────────────────

// NoopProvider silently discards all emails. Useful for tests.
type NoopProvider struct{}

func (p *NoopProvider) Send(to, subject, textBody, htmlBody string) error { return nil }

// ── SMTPProvider ───────────────────────────────────────────────────────────

// SMTPConfig holds SMTP configuration.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // email address
	FromName string // display name
}

// SMTPProvider sends emails via SMTP.
type SMTPProvider struct {
	cfg SMTPConfig
}

// NewSMTPProvider creates an SMTP email provider.
func NewSMTPProvider(cfg SMTPConfig) (*SMTPProvider, error) {
	if cfg.Host == "" || cfg.Port == 0 || cfg.From == "" {
		return nil, fmt.Errorf("email: Host, Port, and From are required")
	}
	return &SMTPProvider{cfg: cfg}, nil
}

func (p *SMTPProvider) Send(to, subject, textBody, htmlBody string) error {
	from := p.cfg.From
	if p.cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", p.cfg.FromName, p.cfg.From)
	}

	var msg strings.Builder
	msg.WriteString("From: " + from + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")

	if htmlBody != "" {
		boundary := "==DONKEY_BOUNDARY=="
		msg.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n")
		msg.WriteString("--" + boundary + "\r\n")
		msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
		msg.WriteString(textBody + "\r\n\r\n")
		msg.WriteString("--" + boundary + "\r\n")
		msg.WriteString("Content-Type: text/html; charset=\"utf-8\"\r\n\r\n")
		msg.WriteString(htmlBody + "\r\n\r\n")
		msg.WriteString("--" + boundary + "--\r\n")
	} else {
		msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n\r\n")
		msg.WriteString(textBody + "\r\n")
	}

	addr := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	var auth smtp.Auth
	if p.cfg.Username != "" {
		auth = smtp.PlainAuth("", p.cfg.Username, p.cfg.Password, p.cfg.Host)
	}

	return smtp.SendMail(addr, auth, p.cfg.From, []string{to}, []byte(msg.String()))
}

// ── NewProvider ────────────────────────────────────────────────────────────

// NewProvider creates an email provider based on config.
// Returns SMTP if Host is set, LogProvider otherwise.
func NewProvider(cfg SMTPConfig) (Provider, error) {
	if cfg.Host == "" {
		log.Printf("[email] no SMTP host — using log provider")
		return &LogProvider{}, nil
	}
	return NewSMTPProvider(cfg)
}

// ── Template Renderer ──────────────────────────────────────────────────────

// Template holds a named email template.
type Template struct {
	Subject string // Go text/template string
	HTML    string // Go html/template string
	Text    string // Go text/template string (plaintext fallback)
}

// Renderer renders email templates with data.
type Renderer struct {
	templates map[string]*Template
}

// NewRenderer creates a new template renderer.
func NewRenderer() *Renderer {
	return &Renderer{templates: make(map[string]*Template)}
}

// Register adds a named template.
func (r *Renderer) Register(name string, tmpl Template) {
	r.templates[name] = &tmpl
}

// Render executes a named template with data and returns subject, HTML, and text.
func (r *Renderer) Render(name string, data map[string]any) (subject, html, text string, err error) {
	tmpl, ok := r.templates[name]
	if !ok {
		return "", "", "", fmt.Errorf("email template %q not found", name)
	}

	subject, err = renderText("subject", tmpl.Subject, data)
	if err != nil {
		return "", "", "", fmt.Errorf("email subject template: %w", err)
	}

	if tmpl.HTML != "" {
		html, err = renderHTML("html", tmpl.HTML, data)
		if err != nil {
			return "", "", "", fmt.Errorf("email html template: %w", err)
		}
	}

	if tmpl.Text != "" {
		text, err = renderText("text", tmpl.Text, data)
		if err != nil {
			return "", "", "", fmt.Errorf("email text template: %w", err)
		}
	}

	return subject, html, text, nil
}

func renderText(name, tmplStr string, data map[string]any) (string, error) {
	t, err := texttmpl.New(name).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func renderHTML(name, tmplStr string, data map[string]any) (string, error) {
	t, err := htmltmpl.New(name).Parse(tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
