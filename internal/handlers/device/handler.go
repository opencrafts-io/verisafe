package device

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/geo"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	devicesvc "github.com/opencrafts-io/verisafe/internal/service/device"
)

type DeviceHandler struct {
	GeoLocator geo.IPLocater
	DB         core.IDBProvider
	Cacher     core.Cacher
	Logger     *slog.Logger
	Cfg        *config.Config
}

func (dh *DeviceHandler) RegisterHandlers(router core.Router) {
	router.Handle(
		"GET /devices/mine",
		middleware.CreateStack(
			middleware.IsAuthenticated(dh.Cfg, dh.DB, dh.Cacher, dh.Logger),
		)(
			core.AppHandler(dh.GetPersonalDevices),
		),
	)
}

// GetPersonalDevices godoc
//
// @Summary      List authenticated user's devices
// @Description  Returns all devices registered to the currently authenticated user
// @Tags         devices
// @Produce      json
// @Success      200  {array}   devicesvc.DeviceOutput
// @Failure      401  {object}  core.APIError  "Unauthorized"
// @Failure      500  {object}  core.APIError  "Internal server error"
// @Security     BearerToken
// @Security     ApiKey
// @Router       /devices/mine [get]
func (dh *DeviceHandler) GetPersonalDevices(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok {
		return fmt.Errorf("%w: missing claims", core.ErrUnauthorized)
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		dh.Logger.Error("Error while parsing user id", slog.Any("error", err))
		return err
	}
	conn, err := dh.DB.Acquire(r.Context())
	if err != nil {
		dh.Logger.Error(
			"Failed to acquire db connection",
			slog.Any("error", err),
		)
		return fmt.Errorf("%w: failed to acquire connection", core.ErrInternal)
	}

	var userDevices []devicesvc.DeviceOutput

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		svc := devicesvc.NewDeviceService(repository.New(tx))
		userDevices, err = svc.RetrieveAllUserDevices(r.Context(), userID)
		return err
	})
	if err != nil {
		dh.Logger.Error(
			"Error occurred while fetching user devices.",
			slog.String("user_id", userID.String()),
			slog.Any("error", err),
		)
		return err
	}

	core.WriteJSON(w, http.StatusOK, userDevices)
	return nil
}
