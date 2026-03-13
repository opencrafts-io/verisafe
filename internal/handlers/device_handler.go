package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/service"
)

type DeviceHandler struct {
	DB     core.IDBProvider
	Logger *slog.Logger
}

func (dh *DeviceHandler) RegisterRoutes(
	config *config.Config,
	router *http.ServeMux,
) {
	router.Handle(
		"POST /devices/add",
		middleware.CreateStack(
			middleware.IsAuthenticated(config, dh.Logger),
		)(
			AppHandler(dh.RegisterUserDevice),
		),
	)
}

func (dh *DeviceHandler) RegisterUserDevice(
	w http.ResponseWriter,
	r *http.Request,
) error {
	var input service.DeviceRegistrationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		return fmt.Errorf(
			"%w: please check your request body",
			core.ErrInvalidInput,
		)
	}

	var device *service.DeviceOutput

	conn, err := dh.DB.Acquire(r.Context())
	if err != nil {
		dh.Logger.Error(
			"Failed to acquire db connection",
			slog.Any("error", err),
		)
		return fmt.Errorf("%w: failed to acquire connection", core.ErrInternal)
	}

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		svc := service.NewDeviceService(repository.New(tx))
		var err error
		device, err = svc.RegisterDevice(r.Context(), input)
		return err
	})
	if err != nil {
		dh.Logger.Error(
			"Failed to create new user device",
			slog.Any("error", err),
		)
		return err
	}

	writeJSON(w, http.StatusCreated, device)
	return nil
}
