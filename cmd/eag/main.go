package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/vectorcore/eag/internal/api"
	"github.com/vectorcore/eag/internal/config"
	"github.com/vectorcore/eag/internal/db"
	"github.com/vectorcore/eag/internal/expiry"
	"github.com/vectorcore/eag/internal/feeds"
	"github.com/vectorcore/eag/internal/xmpp"
	webui "github.com/vectorcore/eag/web"
)

var version = "dev"

func main() {
	cfgPath := flag.String("c", "config.yaml", "path to config file")
	showVer := flag.Bool("v", false, "print version and exit")
	debug   := flag.Bool("d", false, "force debug log level, output to console")
	flag.Parse()

	if *showVer {
		fmt.Printf("VectorCore EAG %s\n", version)
		os.Exit(0)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: load config: %v\n", err)
		os.Exit(1)
	}

	// Configure logger — -d forces debug level to stdout regardless of yaml settings.
	var logLevel slog.Level
	switch cfg.Log.Level {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	// File writer — always active when log.file is set, at the yaml level.
	var fileWriter io.Writer = io.Discard
	if cfg.Log.File != "" {
		f, err := os.OpenFile(cfg.Log.File, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fatal: open log file %q: %v\n", cfg.Log.File, err)
			os.Exit(1)
		}
		defer f.Close()
		fileWriter = f
	}

	// logWriter is what all log consumers (slog + chi) write to.
	// -d adds stdout on top of the file writer.
	var logWriter io.Writer
	if *debug {
		logLevel = slog.LevelDebug
		logWriter = io.MultiWriter(fileWriter, os.Stdout)
	} else {
		logWriter = fileWriter
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: logLevel})))

	// Database
	database, err := db.Init(cfg.Database.Driver, cfg.Database.DSN)
	if err != nil {
		slog.Error("fatal: init database", "error", err)
		os.Exit(1)
	}

	// Root context — cancelled on SIGINT/SIGTERM
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("shutdown signal received")
		cancel()
	}()

	startAt := time.Now().Unix()

	// XMPP server
	xmppServer, err := xmpp.NewServer(&cfg.XMPPServer, database)
	if err != nil {
		slog.Error("fatal: init xmpp server", "error", err)
		os.Exit(1)
	}
	if err := xmppServer.Start(ctx); err != nil {
		slog.Error("fatal: start xmpp server", "error", err)
		os.Exit(1)
	}

	// Feed manager — uses the XMPP server as the alert broadcaster
	manager := feeds.NewManager(database, &cfg.Feeds, xmppServer)
	manager.Start(ctx)

	// Expiry worker
	expiryWorker := expiry.NewWorker(database, cfg.Expiry.SweepInterval, cfg.Expiry.HardDeleteAfter)
	go expiryWorker.Run(ctx)

	// API server
	server := api.NewServer(database, manager, xmppServer, expiryWorker, &cfg.XMPPServer, startAt, version, webui.FS, logWriter)
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	slog.Info("starting VectorCore EAG", "addr", addr, "db_driver", cfg.Database.Driver)

	if err := server.Start(ctx, addr); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("shutdown complete")
}
