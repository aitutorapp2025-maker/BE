package handler

import (
	"errors"
	"strconv"
	"strings"

	"github.com/aitutorapp2025-maker/vaha-backend/internal/model"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/repository"
	"github.com/aitutorapp2025-maker/vaha-backend/internal/service"
	"github.com/gofiber/fiber/v2"
)

// AdminUserHandler manages admin accounts and their roles/permissions
// (the "Admin users" section of the panel).
type AdminUserHandler struct {
	svc    *service.AdminUserService
	admins *repository.AdminRepository
	roles  *repository.AdminRoleRepository
}

// NewAdminUserHandler builds an AdminUserHandler.
func NewAdminUserHandler(
	svc *service.AdminUserService,
	admins *repository.AdminRepository,
	roles *repository.AdminRoleRepository,
) *AdminUserHandler {
	return &AdminUserHandler{svc: svc, admins: admins, roles: roles}
}

// ─── Admin accounts ─────────────────────────────────────────────────────────

// List returns every admin account with its role resolved.
// GET /admin/admins
func (h *AdminUserHandler) List(c *fiber.Ctx) error {
	admins, err := h.admins.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load admins")
	}
	for i := range admins {
		admins[i].ApplyRole()
	}
	return c.JSON(fiber.Map{"success": true, "admins": admins})
}

type adminUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	RoleID   *uint  `json:"role_id"` // nil = super admin
	IsActive *bool  `json:"is_active"`
}

// Create makes a new admin account; a temporary password is generated and
// emailed to them. When email isn't configured, the password is returned once
// in the response instead.
// POST /admin/admins  {name, email, role_id}
func (h *AdminUserHandler) Create(c *fiber.Ctx) error {
	var req adminUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Name == "" || req.Email == "" || !strings.Contains(req.Email, "@") {
		return fiber.NewError(fiber.StatusBadRequest, "name and a valid email are required")
	}
	// Only a super admin can create another super admin (no role).
	actor, err := h.actor(c)
	if err != nil {
		return err
	}
	if req.RoleID == nil && actor.RoleID != nil {
		return fiber.NewError(fiber.StatusForbidden,
			"only a super admin can create a super admin — pick a role")
	}

	res, err := h.svc.Create(req.Name, req.Email, req.RoleID)
	if err != nil {
		if errors.Is(err, service.ErrEmailTaken) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return fiber.NewError(fiber.StatusInternalServerError, "failed to create admin")
	}
	out := fiber.Map{"success": true, "admin": res.Admin, "emailed": res.Emailed}
	if res.TempPassword != "" {
		out["temp_password"] = res.TempPassword
		out["warning"] = "Email is not configured — share this temporary password with the new admin now; it won't be shown again."
	}
	return c.JSON(out)
}

// Update edits an admin's name/email/role/active flag.
// PUT /admin/admins/:id
func (h *AdminUserHandler) Update(c *fiber.Ctx) error {
	id, target, err := h.target(c)
	if err != nil {
		return err
	}
	var req adminUserRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Name == "" || req.Email == "" || !strings.Contains(req.Email, "@") {
		return fiber.NewError(fiber.StatusBadRequest, "name and a valid email are required")
	}

	actor, err := h.actor(c)
	if err != nil {
		return err
	}
	// Non-supers can't touch a super admin, grant super, or change their own role.
	if actor.RoleID != nil {
		if target.RoleID == nil {
			return fiber.NewError(fiber.StatusForbidden, "only a super admin can edit a super admin")
		}
		if req.RoleID == nil {
			return fiber.NewError(fiber.StatusForbidden, "only a super admin can grant super access")
		}
	}
	if actor.ID == target.ID {
		// Editing yourself: keep your own role/active state (no self-demotion
		// or self-lockout by accident).
		req.RoleID = target.RoleID
		active := target.IsActive
		req.IsActive = &active
	}
	// Never remove/deactivate the last active super admin.
	if target.RoleID == nil && (req.RoleID != nil || (req.IsActive != nil && !*req.IsActive)) {
		n, err := h.admins.CountSupers()
		if err == nil && n <= 1 {
			return fiber.NewError(fiber.StatusBadRequest, service.ErrLastSuperAdmin.Error())
		}
	}
	// Email must stay unique.
	if other, err := h.admins.FindByEmail(req.Email); err == nil && other.ID != id {
		return fiber.NewError(fiber.StatusBadRequest, service.ErrEmailTaken.Error())
	}
	if req.RoleID != nil {
		if _, err := h.roles.FindByID(*req.RoleID); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "role not found")
		}
	}

	target.Name = req.Name
	target.Email = req.Email
	target.RoleID = req.RoleID
	if req.IsActive != nil {
		target.IsActive = *req.IsActive
	}
	if err := h.admins.Update(target); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to update admin")
	}
	updated, err := h.admins.FindByID(id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to reload admin")
	}
	updated.ApplyRole()
	return c.JSON(fiber.Map{"success": true, "admin": updated})
}

// Delete removes an admin account.
// DELETE /admin/admins/:id
func (h *AdminUserHandler) Delete(c *fiber.Ctx) error {
	id, target, err := h.target(c)
	if err != nil {
		return err
	}
	actor, err := h.actor(c)
	if err != nil {
		return err
	}
	if actor.ID == id {
		return fiber.NewError(fiber.StatusBadRequest, "you can't delete your own account")
	}
	if target.RoleID == nil {
		if actor.RoleID != nil {
			return fiber.NewError(fiber.StatusForbidden, "only a super admin can delete a super admin")
		}
		n, err := h.admins.CountSupers()
		if err == nil && n <= 1 {
			return fiber.NewError(fiber.StatusBadRequest, service.ErrLastSuperAdmin.Error())
		}
	}
	if err := h.admins.Delete(id); err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete admin")
	}
	return c.JSON(fiber.Map{"success": true})
}

// ResetPassword generates a fresh temporary password for an admin and emails
// it to them (returned once in the response when email is off).
// POST /admin/admins/:id/reset-password
func (h *AdminUserHandler) ResetPassword(c *fiber.Ctx) error {
	id, target, err := h.target(c)
	if err != nil {
		return err
	}
	actor, err := h.actor(c)
	if err != nil {
		return err
	}
	if target.RoleID == nil && actor.RoleID != nil {
		return fiber.NewError(fiber.StatusForbidden, "only a super admin can reset a super admin's password")
	}
	res, err := h.svc.ResetPassword(id)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to reset password")
	}
	out := fiber.Map{"success": true, "emailed": res.Emailed}
	if res.TempPassword != "" {
		out["temp_password"] = res.TempPassword
		out["warning"] = "Email is not configured — share this temporary password now; it won't be shown again."
	}
	return c.JSON(out)
}

// ─── Roles ──────────────────────────────────────────────────────────────────

// Permissions returns the grantable permission catalog for the role editor.
// GET /admin/permissions
func (h *AdminUserHandler) Permissions(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "permissions": model.AllPermissions})
}

// ListRoles returns every role with its usage count.
// GET /admin/roles
func (h *AdminUserHandler) ListRoles(c *fiber.Ctx) error {
	roles, err := h.roles.List()
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, "failed to load roles")
	}
	return c.JSON(fiber.Map{"success": true, "roles": roles})
}

type roleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

func (r *roleRequest) validate() error {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return errors.New("role name is required")
	}
	clean := make([]string, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if !model.ValidPermission(p) {
			return errors.New("unknown permission: " + p)
		}
		clean = append(clean, p)
	}
	r.Permissions = clean
	return nil
}

// CreateRole makes a new role.
// POST /admin/roles  {name, description, permissions[]}
func (h *AdminUserHandler) CreateRole(c *fiber.Ctx) error {
	var req roleRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := req.validate(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	role := &model.AdminRole{
		Name:        req.Name,
		Description: strings.TrimSpace(req.Description),
		Permissions: req.Permissions,
	}
	if err := h.roles.Create(role); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "failed to create role (name may already exist)")
	}
	return c.JSON(fiber.Map{"success": true, "role": role})
}

// UpdateRole edits a role's name/description/permissions.
// PUT /admin/roles/:id
func (h *AdminUserHandler) UpdateRole(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid role id")
	}
	role, err := h.roles.FindByID(uint(id))
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, "role not found")
	}
	var req roleRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := req.validate(); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	role.Name = req.Name
	role.Description = strings.TrimSpace(req.Description)
	role.Permissions = req.Permissions
	if err := h.roles.Update(role); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "failed to update role (name may already exist)")
	}
	return c.JSON(fiber.Map{"success": true, "role": role})
}

// DeleteRole removes an unused role.
// DELETE /admin/roles/:id
func (h *AdminUserHandler) DeleteRole(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid role id")
	}
	if err := h.roles.Delete(uint(id)); err != nil {
		if errors.Is(err, repository.ErrRoleInUse) {
			return fiber.NewError(fiber.StatusBadRequest,
				"this role is assigned to one or more admins — reassign them first")
		}
		return fiber.NewError(fiber.StatusInternalServerError, "failed to delete role")
	}
	return c.JSON(fiber.Map{"success": true})
}

// ─── helpers ────────────────────────────────────────────────────────────────

func (h *AdminUserHandler) actor(c *fiber.Ctx) (*model.Admin, error) {
	adminID, _ := c.Locals("admin_id").(uint)
	actor, err := h.admins.FindByID(adminID)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "account not found")
	}
	return actor, nil
}

func (h *AdminUserHandler) target(c *fiber.Ctx) (uint, *model.Admin, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return 0, nil, fiber.NewError(fiber.StatusBadRequest, "invalid admin id")
	}
	target, err := h.admins.FindByID(uint(id))
	if err != nil {
		return 0, nil, fiber.NewError(fiber.StatusNotFound, "admin not found")
	}
	return uint(id), target, nil
}
