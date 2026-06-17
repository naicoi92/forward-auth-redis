package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pquerna/otp"
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

	var (
		secret     string
		otpauthURI string
		key        *otp.Key
	)

	if providedSecret != "" {
		secret = providedSecret
		otpauthURI = buildURI(cfg.TOTPIssuer, username, secret)
		var err error
		key, err = otp.NewKeyFromURL(otpauthURI)
		if err != nil {
			fmt.Fprintf(os.Stderr, "build key from uri: %v\n", err)
			os.Exit(1)
		}
	} else {
		var err error
		key, err = totp.Generate(totp.GenerateOpts{
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

	qrImg, err := key.Image(60, 60)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate qr image: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "QR code:")
	printQR(os.Stderr, qrImg)

	fmt.Fprintln(os.Stderr, "Keep the secret safe and add it to your authenticator app.")
}

// printQR renders an image.Image QR code to w using Unicode half-blocks.
// Each terminal row encodes two image rows, producing a compact scannable code.
func printQR(w io.Writer, img image.Image) {
	bounds := img.Bounds()

	// QR images are usually grayscale: dark pixels are modules, light pixels are background.
	isDark := func(x, y int) bool {
		c := color.GrayModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray)
		return c.Y < 128
	}

	for y := bounds.Min.Y; y < bounds.Max.Y; y += 2 {
		var b strings.Builder
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			upper := isDark(x, y)
			lower := false
			if y+1 < bounds.Max.Y {
				lower = isDark(x, y+1)
			}
			switch {
			case upper && lower:
				b.WriteRune('█')
			case upper:
				b.WriteRune('▀')
			case lower:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		fmt.Fprintln(w, b.String())
	}
}

func buildURI(issuer, username, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s:%s?secret=%s\u0026issuer=%s",
		url.QueryEscape(issuer),
		url.QueryEscape(username),
		secret,
		url.QueryEscape(issuer),
	)
}
