package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"unicode/utf8"

	"cpa-usage-ranking/internal/auth"
	"cpa-usage-ranking/internal/profiles"
	"cpa-usage-ranking/internal/registration"
	"github.com/gin-gonic/gin"
)

type RegistrationHTTPOptions struct {
	GroupIPv6By64 bool
	MaxBodyBytes  int64
}

// MountRegistration exposes creation only behind the full request-admission
// gate. The service owns retries, disabled status, creation quotas and switch.
func MountRegistration(router *gin.Engine, service *registration.Service, limiter *registration.RequestLimiter, options RegistrationHTTPOptions) error {
	if options.MaxBodyBytes <= 0 {
		return fmt.Errorf("registration body limit must be positive")
	}
	router.POST("/v1/clients/register", RegistrationAdmission(limiter, options.GroupIPv6By64), func(c *gin.Context) {
		hash, err := auth.BearerHash(c.Request.Header)
		if err != nil {
			c.Header("WWW-Authenticate", "Bearer")
			writeError(c, http.StatusUnauthorized, "invalid_credential", "Invalid client credential")
			return
		}
		mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || mediaType != "application/json" || (c.GetHeader("Content-Encoding") != "" && c.GetHeader("Content-Encoding") != "identity") {
			writeError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use uncompressed application/json")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, options.MaxBodyBytes))
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Registration body is too large")
			return
		}
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "Invalid registration request")
			return
		}
		profile, err := decodeRegistration(body)
		if err != nil {
			writeError(c, http.StatusBadRequest, "invalid_request", "Invalid registration request")
			return
		}
		// 来源由前置中间件建立，不读取 JSON、query 或调用方提供的 client_id。
		source := c.MustGet(registrationSourceKey).(string)
		result, err := service.Register(c.Request.Context(), source, hash, profile)
		switch {
		case errors.Is(err, registration.ErrDisabled):
			writeError(c, http.StatusForbidden, "client_disabled", "Client is disabled")
		case errors.Is(err, registration.ErrRegistrationClosed):
			writeError(c, http.StatusServiceUnavailable, "registration_closed", "New identity registration is closed")
		case errors.Is(err, profiles.ErrInvalidDisplayName):
			writeError(c, http.StatusUnprocessableEntity, "invalid_display_name", "Invalid display name")
		case errors.Is(err, registration.ErrInvalidAvatar):
			writeError(c, http.StatusUnprocessableEntity, "invalid_avatar", "Invalid avatar selection")
		case err != nil:
			var limit *registration.LimitError
			if errors.As(err, &limit) {
				writeRegistrationLimit(c, err)
			} else {
				writeError(c, http.StatusInternalServerError, "internal_error", "Internal server error")
			}
		default:
			status := http.StatusOK
			if result.Created {
				status = http.StatusCreated
			}
			c.JSON(status, gin.H{"client_id": result.Client.ID, "display_name": result.Client.DisplayName, "avatar_id": result.Client.AvatarID})
		}
	})
	return nil
}

func decodeRegistration(body []byte) (registration.Profile, error) {
	trimmed := bytes.TrimSpace(body)
	if !utf8.Valid(body) || len(trimmed) == 0 || trimmed[0] != '{' {
		return registration.Profile{}, fmt.Errorf("expected a UTF-8 JSON object")
	}
	var input struct {
		DisplayName string          `json:"display_name"`
		AvatarID    json.RawMessage `json:"avatar_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return registration.Profile{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return registration.Profile{}, fmt.Errorf("expected exactly one JSON object")
	}
	profile := registration.Profile{DisplayName: input.DisplayName}
	if len(input.AvatarID) > 0 {
		var avatarID int64
		if err := json.Unmarshal(input.AvatarID, &avatarID); err != nil {
			return registration.Profile{}, err
		}
		// JSON null 与显式 0 一样不表示省略；仅缺少字段时才使用目录默认值。
		profile.AvatarID = &avatarID
	}
	return profile, nil
}
