// Command daumdev builds and serves the read-only daum.dev code site.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"daum.dev/showcase/internal/build"
	"daum.dev/showcase/internal/config"
)

func main() {
	cfgPath := flag.String("config", "/config.json", "path to config.json")
	once := flag.Bool("once", false, "render the site once into -out and exit")
	out := flag.String("out", "", "output directory for -once (must not exist)")
	data := flag.String("data", "/data", "data directory (git mirrors, site builds)")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !*once {
		fatal(errors.New("server mode is not implemented yet; use -once -out DIR"))
	}
	if *out == "" {
		fatal(errors.New("-once needs -out"))
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		fatal(err)
	}
	if err := build.New(cfg, *data).Render(ctx, *out); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	slog.Error(err.Error())
	os.Exit(1)
}
