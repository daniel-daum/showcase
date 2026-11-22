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
	"time"

	"daum.dev/showcase/internal/build"
	"daum.dev/showcase/internal/config"
	"daum.dev/showcase/internal/server"
)

func main() {
	cfgPath := flag.String("config", "/config.json", "path to config.json")
	once := flag.Bool("once", false, "render the site once into -out and exit")
	out := flag.String("out", "", "output directory for -once (must not exist)")
	data := flag.String("data", "/data", "data directory (git mirrors, site builds)")
	every := flag.Duration("interval", 10*time.Minute, "time between builds")
	listen := flag.String("listen", ":8080", "address to serve on")
	serveDir := flag.String("serve-dir", "", "dev: serve this finished build instead of building")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	b := build.New(cfg, *data)
	if !*once {
		srv := server.New(cfg, b.Current, b.Ready, slog.Default())
		if *serveDir != "" {
			srv.Root, srv.Ready = func() string { return *serveDir }, func() bool { return true }
		} else {
			go b.Run(ctx, *every)
		}
		slog.Info("listening", "addr", *listen)
		if err := srv.ListenAndServe(ctx, *listen); err != nil {
			fatal(err)
		}
		return
	}
	if *out == "" {
		fatal(errors.New("-once needs -out"))
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		fatal(err)
	}
	if err := b.Render(ctx, *out); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	slog.Error(err.Error())
	os.Exit(1)
}
