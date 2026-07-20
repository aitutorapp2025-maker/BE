package handler

import (
	"errors"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// StudentAuthHandler handles passwordless student login (OTP over SMS) for the
// mobile app.
type StudentAuthHandler struct {
	auth *service.StudentAuthService
}

// NewStudentAuthHandler builds a StudentAuthHandler.
func NewStudentAuthHandler(auth *service.StudentAuthService) *StudentAuthHandler {
	return &StudentAuthHandler{auth: auth}
}

type sendOtpRequest struct {
	Phone string `json:"phone"`
}

// SendOTP sends a 6-digit code by SMS to the given phone number.
//
// POST /api/v1/student/send-otp  { "phone": "9876543210" }
func (h *StudentAuthHandler) SendOTP(c *fiber.Ctx) error {
	var req sendOtpRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	devCode, err := h.auth.SendOTP(c.Context(), req.Phone)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidPhone):
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrOTPThrottled):
			return fiber.NewError(fiber.StatusTooManyRequests, err.Error())
		default:
			return fiber.NewError(fiber.StatusInternalServerError, "could not send the code — please try again")
		}
	}

	resp := fiber.Map{"success": true, "message": "Verification code sent"}
	// Only present in non-production, so the app can be tested without a live
	// SMS gateway. Never returned in production.
	if devCode != "" {
		resp["dev_code"] = devCode
	}
	return c.JSON(resp)
}

type verifyOtpRequest struct {
	Phone       string `json:"phone"`
	Code        string `json:"code"`
	DeviceToken string `json:"device_token"` // optional FCM token from the device
}

// VerifyOTP checks the code and, on success, signs the student in.
//
// POST /api/v1/student/verify-otp  { "phone": "...", "code": "123456", "device_token": "..." }
func (h *StudentAuthHandler) VerifyOTP(c *fiber.Ctx) error {
	var req verifyOtpRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Code) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "code is required")
	}

	result, err := h.auth.VerifyOTP(c.Context(), req.Phone, req.Code, req.DeviceToken)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidPhone):
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrInvalidOTP):
			return fiber.NewError(fiber.StatusUnauthorized, err.Error())
		default:
			return fiber.NewError(fiber.StatusInternalServerError, "verification failed — please try again")
		}
	}

	return c.JSON(fiber.Map{
		"success":    true,
		"token":      result.Token,
		"token_type": "Bearer",
		"expires_at": result.ExpiresAt,
		"is_new":     result.IsNew,
		"student":    result.Student,
	})
}

type registerDeviceRequest struct {
	Token    string `json:"token"`
	Platform string `json:"platform"`
}

// RegisterDevice records an FCM push token at app open, before login. The token
// is stored unmapped and later associated with a student on OTP verification.
//
// POST /api/v1/student/register-device  { "token": "...", "platform": "android" }
func (h *StudentAuthHandler) RegisterDevice(c *fiber.Ctx) error {
	var req registerDeviceRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Token) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "token is required")
	}
	if err := h.auth.RegisterDevice(c.Context(), req.Token, req.Platform); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not register the device")
	}
	return c.JSON(fiber.Map{"success": true})
}

type deviceTokenRequest struct {
	DeviceToken string `json:"device_token"`
}

// SaveDeviceToken maps the FCM push token to the signed-in student + mobile.
//
// POST /api/v1/student/device-token  { "device_token": "..." }  (Bearer student JWT)
func (h *StudentAuthHandler) SaveDeviceToken(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	phone, _ := c.Locals("phone").(string)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}

	var req deviceTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.auth.SaveDeviceToken(c.Context(), studentID, phone, req.DeviceToken); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save the device token")
	}
	return c.JSON(fiber.Map{"success": true})
}
