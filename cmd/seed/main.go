package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/naicoi92/forward-auth-redis/internal/config"
	"github.com/naicoi92/forward-auth-redis/internal/redisx"
	"github.com/naicoi92/forward-auth-redis/internal/userx"
)

func main() {
	var providedSecret string
	flag.StringVar(&providedSecret, "secret", "", "optional pre-generated base32 TOTP secret")
	flag.Parse()

	if len(flag.Args()) != 1 {
		fmt.Fprintln(os.Stderr, "usage: seed [-secret=SECRET] <username>")
		os.Exit(1)
	}
	username := flag.Args()[0]
	if err := userx.ValidateUsername(username); err != nil {
		fmt.Fprintf(os.Stderr, "invalid username: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	var secret string
	var otpauthURI string

	if providedSecret != "" {
		secret = providedSecret
		otpauthURI = buildURI(cfg.TOTPIssuer, username, secret)
	} else {
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      cfg.TOTPIssuer,
			AccountName: username,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate secret: %v\n", err)
			os.Exit(1)
		}
		secret = key.Secret()
		otpauthURI = key.URL()
	}

	clients, err := redisx.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "redis connect: %v\n", err)
		os.Exit(1)
	}
	defer clients.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := clients.Writer.HSet(ctx, "totp:secrets", username, secret).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "redis hset: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Seeded user: %s\n", username)
	fmt.Fprintf(os.Stderr, "Secret:    %s\n", secret)
	fmt.Printf("URI:       %s\n", otpauthURI)
	fmt.Fprintln(os.Stderr, "Keep the secret safe and add it to your authenticator app.")
}

func buildURI(issuer, username, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s\u0026issuer=%s",
		url.QueryEscape(issuer),
		url.QueryEscape(username),
		secret,
		url.QueryEscape(issuer),
	)
}
