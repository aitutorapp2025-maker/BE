package handler

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/gofiber/fiber/v2"
)

// StudentHandler exposes CRUD endpoints for students (admin only).
type StudentHandler struct {
	students *repository.StudentRepository
}

// NewStudentHandler builds a StudentHandler.
func NewStudentHandler(students *repository.StudentRepository) *StudentHandler {
	return &StudentHandler{students: students}
}

// studentRequest is the JSON body for create/update.
type studentRequest struct {
	Name             string     `json:"name"`
	Phone            string     `json:"phone"`
	ParentPhone      string     `json:"parent_phone"`
	StudentClass     string     `json:"student_class"`
	Board            string     `json:"board"`
	Medium           string     `json:"medium"`
	TeachingLanguage string     `json:"teaching_language"`
	Plan             string     `json:"plan"`
	PayStatus        string     `json:"pay_status"`
	JoinedAt         *time.Time `json:"joined_at"`
}

var (
	validPlans   = map[string]bool{"trial": true, "monthly": true, "yearly": true}
	validPayStat = map[string]bool{"trial": true, "paid": true, "expired": true}
)

// List returns all students. GET /api/v1/admin/students
func (h *StudentHandler) List(c *fiber.Ctx) error {
	students, err := h.students.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load students")
	}
	return c.JSON(fiber.Map{"success": true, "students": students})
}

// Get returns a single student. GET /api/v1/admin/students/:id
func (h *StudentHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	s, err := h.students.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "student")
	}
	return c.JSON(fiber.Map{"success": true, "student": s})
}

// Create adds a student. POST /api/v1/admin/students
func (h *StudentHandler) Create(c *fiber.Ctx) error {
	req, err := parseStudentBody(c)
	if err != nil {
		return err
	}

	joined := time.Now()
	if req.JoinedAt != nil {
		joined = *req.JoinedAt
	}
	s := &model.Student{
		Name:             req.Name,
		Phone:            req.Phone,
		ParentPhone:      req.ParentPhone,
		StudentClass:     req.StudentClass,
		Board:            req.Board,
		Medium:           req.Medium,
		TeachingLanguage: req.TeachingLanguage,
		Plan:             req.Plan,
		PayStatus:        req.PayStatus,
		JoinedAt:         joined,
	}
	if err := h.students.Create(s); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create student")
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"success": true, "student": s})
}

// Update edits a student. PUT /api/v1/admin/students/:id
func (h *StudentHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	s, err := h.students.FindByID(id)
	if err != nil {
		return notFoundOrInternal(err, "student")
	}

	req, err := parseStudentBody(c)
	if err != nil {
		return err
	}

	s.Name = req.Name
	s.Phone = req.Phone
	s.ParentPhone = req.ParentPhone
	s.StudentClass = req.StudentClass
	s.Board = req.Board
	s.Medium = req.Medium
	s.TeachingLanguage = req.TeachingLanguage
	s.Plan = req.Plan
	s.PayStatus = req.PayStatus
	if req.JoinedAt != nil {
		s.JoinedAt = *req.JoinedAt
	}

	if err := h.students.Update(s); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update student")
	}
	return c.JSON(fiber.Map{"success": true, "student": s})
}

// Delete removes a student. DELETE /api/v1/admin/students/:id
func (h *StudentHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.students.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete student")
	}
	return c.JSON(fiber.Map{"success": true})
}

// parseStudentBody parses and validates the request body, applying defaults.
func parseStudentBody(c *fiber.Ctx) (*studentRequest, error) {
	var req studentRequest
	if err := c.BodyParser(&req); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fiber.NewError(fiber.StatusBadRequest, "name is required")
	}
	if req.Plan == "" || !validPlans[req.Plan] {
		req.Plan = "trial"
	}
	if req.PayStatus == "" || !validPayStat[req.PayStatus] {
		req.PayStatus = "trial"
	}
	return &req, nil
}

// parseID reads the :id path param as an unsigned integer.
func parseID(c *fiber.Ctx) (uint, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return 0, fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	return uint(id), nil
}

// notFoundOrInternal maps repository errors to HTTP errors.
func notFoundOrInternal(err error, entity string) error {
	if errors.Is(err, repository.ErrNotFound) {
		return fiber.NewError(fiber.StatusNotFound, entity+" not found")
	}
	return fiber.NewError(fiber.StatusInternalServerError, "request failed")
}
