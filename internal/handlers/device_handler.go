package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/opencrafts-io/verisafe/internal/config"
	"github.com/opencrafts-io/verisafe/internal/core"
	"github.com/opencrafts-io/verisafe/internal/geo"
	"github.com/opencrafts-io/verisafe/internal/middleware"
	"github.com/opencrafts-io/verisafe/internal/repository"
	"github.com/opencrafts-io/verisafe/internal/service"
	"github.com/opencrafts-io/verisafe/internal/utils"
)

type DeviceHandler struct {
	GeoLocator *geo.GeoIPLocater
	DB         core.IDBProvider
	Logger     *slog.Logger
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

	router.Handle(
		"GET /devices/mine",
		middleware.CreateStack(
			middleware.IsAuthenticated(config, dh.Logger),
		)(
			AppHandler(dh.GetPersonalDevices),
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
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		dh.Logger.Error("Error while parsing user id", slog.Any("error", err))
		return err
	}
	input.UserID = userID

	ip, err := netip.ParseAddr(strings.Split(r.RemoteAddr, ":")[0])
	if err != nil {
		return fmt.Errorf(
			"remote ip addr %w: please check your request body",
			core.ErrInvalidInput,
		)
	}

	input.IpAddress = &ip

	lookupInfo, err := dh.GeoLocator.Lookup(ip)
	if err != nil {
		dh.Logger.Error(
			"Failed to get lookupInfo from host ip address.",
			slog.Any("error", err),
			slog.String("ip", ip.String()),
		)
	} else {
		input.Country = &lookupInfo.Country.ISOCode
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

func (dh *DeviceHandler) GetPersonalDevices(
	w http.ResponseWriter,
	r *http.Request,
) error {
	claims := r.Context().Value(middleware.AuthUserClaims).(*utils.VerisafeClaims)
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

	var userDevices []service.DeviceOutput

	err = core.WithTransaction(r.Context(), conn, func(tx pgx.Tx) error {
		svc := service.NewDeviceService(repository.New(tx))
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

	writeJSON(w, http.StatusOK, userDevices)
	return nil
}
