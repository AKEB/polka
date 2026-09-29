// Package config holds the server configuration, collected from
// command-line flags with environment-variable fallbacks (POLKA_*).
package config

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	// Addr is the HTTP listen address, e.g. ":12791" or "127.0.0.1:8080".
	Addr string
	// DataDir holds the SQLite database and caches.
	DataDir string
	// LibraryDir is the root directory with book files and archives.
	LibraryDir string
	// Auth: "required" — catalog only after login, "public" — open,
	// "demo" — public showcase with ephemeral guest sessions.
	Auth string
	// PublicURL is the externally visible base URL (https://polka.example.com),
	// used to build the OIDC redirect URI when --oidc-redirect-url is empty.
	PublicURL string
	// OIDC (optional). When Issuer and ClientID are set, the login page
	// offers "Sign in with …" and new users are created on first login.
	OIDCIssuer       string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string
	OIDCName         string // button label, e.g. "Keycloak"
	// TelegramBotToken enables the Telegram bot when non-empty
	// (BotFather token). Users must have telegram_id set by an admin.
	TelegramBotToken string
	// Version is the build version (set by the binary at startup).
	Version string
}

// OIDCEnabled reports whether OpenID Connect is configured.
func (c *Config) OIDCEnabled() bool {
	return c != nil && c.OIDCIssuer != "" && c.OIDCClientID != ""
}

// OIDCRedirect resolves the callback URL.
func (c *Config) OIDCRedirect() string {
	if c.OIDCRedirectURL != "" {
		return c.OIDCRedirectURL
	}
	base := strings.TrimRight(c.PublicURL, "/")
	if base == "" {
		return ""
	}
	return base + "/auth/oidc/callback"
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Load parses configuration from args (without the program name).
// extra, if provided, registers additional subcommand flags.
// Returns the config and the positional arguments.
func Load(args []string, extra func(*flag.FlagSet)) (*Config, []string, error) {
	defaultData := ".polka"
	if home, err := os.UserHomeDir(); err == nil {
		defaultData = filepath.Join(home, ".polka")
	}

	cfg := &Config{}
	fs := flag.NewFlagSet("polka", flag.ContinueOnError)
	fs.StringVar(&cfg.Addr, "addr", env("POLKA_ADDR", ":12791"), "HTTP listen address")
	fs.StringVar(&cfg.DataDir, "data-dir", env("POLKA_DATA_DIR", defaultData), "directory for database and caches")
	fs.StringVar(&cfg.LibraryDir, "library-dir", env("POLKA_LIBRARY_DIR", ""), "root directory of the book library")
	fs.StringVar(&cfg.Auth, "auth", env("POLKA_AUTH", "required"), `access mode: "required", "public" or "demo"`)
	fs.StringVar(&cfg.PublicURL, "public-url", env("POLKA_PUBLIC_URL", ""), "public base URL (https://…), used for OIDC redirect")
	fs.StringVar(&cfg.OIDCIssuer, "oidc-issuer", env("POLKA_OIDC_ISSUER", ""), "OpenID Connect issuer URL")
	fs.StringVar(&cfg.OIDCClientID, "oidc-client-id", env("POLKA_OIDC_CLIENT_ID", ""), "OIDC client id")
	fs.StringVar(&cfg.OIDCClientSecret, "oidc-client-secret", env("POLKA_OIDC_CLIENT_SECRET", ""), "OIDC client secret")
	fs.StringVar(&cfg.OIDCRedirectURL, "oidc-redirect-url", env("POLKA_OIDC_REDIRECT_URL", ""), "OIDC callback URL (default: <public-url>/auth/oidc/callback)")
	fs.StringVar(&cfg.OIDCName, "oidc-name", env("POLKA_OIDC_NAME", ""), "OIDC button label on the login page")
	fs.StringVar(&cfg.TelegramBotToken, "telegram-token", env("POLKA_TELEGRAM_TOKEN", ""), "Telegram bot token (enables the bot)")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if cfg.OIDCEnabled() && cfg.OIDCRedirect() == "" {
		return nil, nil, fmt.Errorf("OIDC is configured but redirect URL is empty: set --oidc-redirect-url or --public-url")
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create data dir %q: %w (set --data-dir or POLKA_DATA_DIR to a writable path)", cfg.DataDir, err)
	}
	return cfg, fs.Args(), nil
}

// DBPath returns the path to the collection database file.
func (c *Config) DBPath() string {
	return filepath.Join(c.DataDir, "polka.db")
}
