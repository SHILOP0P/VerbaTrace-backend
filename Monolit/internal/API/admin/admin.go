package admin

import "verbatrace/monolit/internal/service"

type Handler struct {
	service service.AdminService
}

func NewHandler(adminService service.AdminService) *Handler {
	return &Handler{service: adminService}
}
