package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"

	"github.com/opencrafts-io/verisafe/internal/app"
	"github.com/opencrafts-io/verisafe/internal/config"
)

// @title           Verisafe API
// @version         2.0
// @description     Verisafe service API
// @termsOfService  http://swagger.io/terms/

// @contact.name   Open Crafts Interactive
// @contact.url    https://opencrafts.io/about
// @contact.email  developers@opencrafts.io

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      https://verisafe.opencrafts.io
// @BasePath  /

// @securityDefinitions.basic  BasicAuth

// @externalDocs.description  OpenAPI
// @externalDocs.url          https://swagger.io/resources/open-api/
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error(
			"Failed to load configuration file",
			slog.Any("error", err),
		)
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
