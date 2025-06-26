package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/opencrafts-io/verisafe/internal/app"
)

func main() {

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := app.LoadConfig()
	if err != nil {
		logger.Error("Failed to load configuration file", slog.Any("error", err))
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	app, err := app.New(logger, cfg)
	if err != nil {
		logger.Error("Failed to create app.", slog.Any("error", err))
	}

	if err := app.Start(ctx); err != nil {
		logger.Error("Failed to start app.", slog.Any("error", err))
	}

}
