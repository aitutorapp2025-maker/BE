package handler

import (
	"errors"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// StudentAuthHandler handles passwordless student login (OTP over SMS) for the
// mobile app.
type StudentAuthHandler struct {
	auth   *service.StudentAuthService
	groups *repository.ClassGroupRepository
}

// NewStudentAuthHandler builds a StudentAuthHandler. The class-group repo is
// used to validate the subject group the student picks during onboarding.
func NewStudentAuthHandler(
	auth *service.StudentAuthService,
	groups *repository.ClassGroupRepository,
) *StudentAuthHandler {
	return &StudentAuthHandler{auth: auth, groups: groups}
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
	ClientPub   string `json:"client_pub"`   // base64 X25519 pubkey for E2E key exchange
}

// VerifyOTP checks the code and, on success, signs the student in — issuing the
// access token, per-session signing secret and E2E server public key.
//
// POST /api/v1/student/verify-otp  { "phone": "...", "code": "123456", "client_pub": "..." }
func (h *StudentAuthHandler) VerifyOTP(c *fiber.Ctx) error {
	var req verifyOtpRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Code) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "code is required")
	}

	result, err := h.auth.VerifyOTP(c.Context(), req.Phone, req.Code, req.DeviceToken, req.ClientPub)
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
		"success":        true,
		"token":          result.Token,
		"token_type":     "Bearer",
		"signing_secret": result.SigningSecret,
		"server_pub":     result.ServerPub,
		"expires_at":     result.ExpiresAt,
		"is_new":         result.IsNew,
		"student":        result.Student,
	})
}

// Me returns the signed-in student's account (used to restore profile state,
// e.g. after a reinstall the app re-fetches whether the profile is complete).
//
// GET /api/v1/student/me  (Bearer student JWT)
func (h *StudentAuthHandler) Me(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	st, err := h.auth.GetStudent(c.Context(), studentID)
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "account not found")
	}
	return c.JSON(fiber.Map{"success": true, "student": st})
}

type updateProfileRequest struct {
	Name             string `json:"name"`
	StudentClass     string `json:"student_class"`
	Board            string `json:"board"`
	Medium           string `json:"medium"`
	StudentGroup     string `json:"student_group"`
	TeachingLanguage string `json:"teaching_language"`
	ParentPhone      string `json:"parent_phone"`
}

// UpdateProfile saves the signed-in student's profile so it persists on the
// server (and is returned on the next login, surviving a reinstall).
//
// PUT /api/v1/student/profile  { "name": "...", "student_class": "...", ... }
func (h *StudentAuthHandler) UpdateProfile(c *fiber.Ctx) error {
	studentID, _ := c.Locals("student_id").(uint)
	if studentID == 0 {
		return fiber.NewError(fiber.StatusUnauthorized, "not signed in")
	}
	var req updateProfileRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.Name) == "" {
		return fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	// Classes that offer subject groups (11 & 12) must have one picked, and it
	// has to be one the admin actually configured for that class + board.
	group, err := h.resolveGroup(req.StudentClass, req.Board, req.StudentGroup)
	if err != nil {
		return err
	}
	st, err := h.auth.UpdateProfile(c.Context(), studentID, service.StudentProfileInput{
		Name:             req.Name,
		StudentClass:     req.StudentClass,
		Board:            req.Board,
		Medium:           req.Medium,
		StudentGroup:     group,
		TeachingLanguage: req.TeachingLanguage,
		ParentPhone:      req.ParentPhone,
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "could not save your profile")
	}
	return c.JSON(fiber.Map{"success": true, "student": st})
}

// resolveGroup validates the subject group chosen during onboarding against the
// groups the admin configured for that class + board. Classes with no groups
// (1–10) simply store a blank group; classes that have them require one.
func (h *StudentAuthHandler) resolveGroup(className, board, chosen string) (string, error) {
	className = strings.TrimSpace(className)
	board = strings.TrimSpace(board)
	chosen = strings.TrimSpace(chosen)
	if className == "" {
		return "", nil
	}
	available, err := h.groups.ListActive(className, board)
	if err != nil {
		return "", fiber.NewError(fiber.StatusInternalServerError, "could not load the groups for your class")
	}
	if len(available) == 0 {
		return "", nil // this class doesn't use groups
	}
	if chosen == "" {
		return "", fiber.NewError(fiber.StatusBadRequest, "please choose your group")
	}
	for _, g := range available {
		if strings.EqualFold(g.Name, chosen) {
			return g.Name, nil
		}
	}
	return "", fiber.NewError(fiber.StatusBadRequest, "that group isn't available for your class")
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
