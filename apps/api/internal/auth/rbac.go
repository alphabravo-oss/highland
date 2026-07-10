package auth

import "net/http"

// MethodAllowed reports whether the role may perform the HTTP method on protected APIs.
// Viewer: GET/HEAD/OPTIONS only. Operator: all except admin-only routes. Admin: all.
func MethodAllowed(role Role, method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return role == RoleOperator || role == RoleAdmin
	default:
		return role == RoleAdmin
	}
}

// AdminOnly is true for Highland admin surfaces (audit, user management).
func AdminOnly(role Role) bool {
	return role == RoleAdmin
}

// ParseRole maps a string to Role (default viewer if unknown).
func ParseRole(s string) Role {
	switch Role(s) {
	case RoleAdmin, RoleOperator, RoleViewer:
		return Role(s)
	default:
		return RoleViewer
	}
}
