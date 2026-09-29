package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsAndOverrides(t *testing.T) {
	// Defaults (with the data dir redirected so the test never touches $HOME).
	dir := t.TempDir()
	t.Setenv("POLKA_ADDR", "")
	t.Setenv("POLKA_LIBRARY_DIR", "")
	t.Setenv("POLKA_AUTH", "")
	t.Setenv("POLKA_DATA_DIR", filepath.Join(dir, "envdata"))
	cfg, rest, err := Load(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":12791" || cfg.Auth != "required" || cfg.LibraryDir != "" || len(rest) != 0 {
		t.Errorf("defaults: %+v rest=%v", cfg, rest)
	}
	if cfg.DataDir != filepath.Join(dir, "envdata") {
		t.Errorf("env data dir: %q", cfg.DataDir)
	}
	if st, err := os.Stat(cfg.DataDir); err != nil || !st.IsDir() {
		t.Error("data dir must be created")
	}
	if cfg.DBPath() != filepath.Join(cfg.DataDir, "polka.db") {
		t.Errorf("DBPath: %q", cfg.DBPath())
	}

	// Environment supplies values…
	t.Setenv("POLKA_ADDR", "127.0.0.1:1")
	t.Setenv("POLKA_LIBRARY_DIR", "/books")
	t.Setenv("POLKA_AUTH", "public")
	cfg, _, _ = Load(nil, nil)
	if cfg.Addr != "127.0.0.1:1" || cfg.LibraryDir != "/books" || cfg.Auth != "public" {
		t.Errorf("env: %+v", cfg)
	}
	// …and flags beat the environment; positional args come back.
	flagData := filepath.Join(dir, "flagdata")
	cfg, rest, err = Load([]string{"--addr", ":9", "--data-dir", flagData, "--library-dir=/lib", "--auth", "demo", "catalog.inpx"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":9" || cfg.DataDir != flagData || cfg.LibraryDir != "/lib" || cfg.Auth != "demo" {
		t.Errorf("flags: %+v", cfg)
	}
	if len(rest) != 1 || rest[0] != "catalog.inpx" {
		t.Errorf("positional: %v", rest)
	}
}

func TestLoadExtraFlagsAndErrors(t *testing.T) {
	t.Setenv("POLKA_DATA_DIR", t.TempDir())
	var replace bool
	var inpx string
	_, rest, err := Load([]string{"--replace", "--inpx", "x.inpx", "pos"}, func(fs *flag.FlagSet) {
		fs.BoolVar(&replace, "replace", false, "")
		fs.StringVar(&inpx, "inpx", "", "")
	})
	if err != nil || !replace || inpx != "x.inpx" || len(rest) != 1 {
		t.Errorf("extra flags: %v %v %q %v", err, replace, inpx, rest)
	}
	if _, _, err := Load([]string{"--no-such-flag"}, nil); err == nil {
		t.Error("unknown flag must be an error")
	}
	// A data dir that cannot be created is reported with a hint.
	blocker := filepath.Join(t.TempDir(), "file")
	os.WriteFile(blocker, []byte("x"), 0o644)
	_, _, err = Load([]string{"--data-dir", filepath.Join(blocker, "sub")}, nil)
	if err == nil || !strings.Contains(err.Error(), "POLKA_DATA_DIR") {
		t.Errorf("unwritable data dir: %v", err)
	}
}

func TestOIDCConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("POLKA_DATA_DIR", dir)
	t.Setenv("POLKA_OIDC_ISSUER", "")
	t.Setenv("POLKA_OIDC_CLIENT_ID", "")
	t.Setenv("POLKA_OIDC_REDIRECT_URL", "")
	t.Setenv("POLKA_PUBLIC_URL", "")

	cfg, _, err := Load(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OIDCEnabled() {
		t.Fatal("OIDC must be off by default")
	}

	// Issuer+client without redirect/public URL is rejected.
	_, _, err = Load([]string{
		"--oidc-issuer", "https://idp.example",
		"--oidc-client-id", "polka",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Errorf("want redirect error, got %v", err)
	}

	cfg, _, err = Load([]string{
		"--oidc-issuer", "https://idp.example",
		"--oidc-client-id", "polka",
		"--public-url", "https://polka.example.com/",
		"--oidc-name", "Keycloak",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OIDCEnabled() || cfg.OIDCName != "Keycloak" {
		t.Fatalf("%+v", cfg)
	}
	if got := cfg.OIDCRedirect(); got != "https://polka.example.com/auth/oidc/callback" {
		t.Errorf("redirect from public-url: %q", got)
	}

	cfg, _, err = Load([]string{
		"--oidc-issuer", "https://idp.example",
		"--oidc-client-id", "polka",
		"--oidc-redirect-url", "https://other.example/cb",
		"--public-url", "https://polka.example.com",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.OIDCRedirect(); got != "https://other.example/cb" {
		t.Errorf("explicit redirect wins: %q", got)
	}
}
