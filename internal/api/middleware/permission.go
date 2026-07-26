package middleware

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/sonicore/server/internal/infrastructure/repository"
)

type rolePerm string

const (
	RoleOwner       = "owner"
	RoleAdmin       = "admin"
	RoleContributor = "contributor"
	RoleViewer      = "viewer"
)

type PermissionChecker struct {
	libRepo *repository.LibraryRepo
}

func NewPermissionChecker(db *sql.DB) *PermissionChecker {
	return &PermissionChecker{
		libRepo: repository.NewLibraryRepo(db),
	}
}

func (pc *PermissionChecker) IsOwner(ctx context.Context, libraryID, userID string) bool {
	lib, err := pc.libRepo.FindByID(ctx, libraryID)
	if err != nil {
		return false
	}
	return lib.OwnerID == userID
}

func (pc *PermissionChecker) HasRole(ctx context.Context, libraryID, userID string, required string) bool {
	if pc.IsOwner(ctx, libraryID, userID) {
		return true
	}

	members, err := pc.libRepo.GetMembers(ctx, libraryID)
	if err != nil {
		return false
	}

	for _, m := range members {
		if m.UserID == userID {
			return roleWeight(m.Role) >= roleWeight(required)
		}
	}
	return false
}

func (pc *PermissionChecker) IsMember(ctx context.Context, libraryID, userID string) bool {
	return pc.HasRole(ctx, libraryID, userID, RoleViewer)
}

func roleWeight(r string) int {
	switch r {
	case RoleOwner:
		return 4
	case RoleAdmin:
		return 3
	case RoleContributor:
		return 2
	case RoleViewer:
		return 1
	}
	return 0
}

func (pc *PermissionChecker) Middleware(required string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := GetUserID(r.Context())
			if userID == "" {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			libID := r.URL.Query().Get("library_id")
			if libID == "" {
				next.ServeHTTP(w, r)
				return
			}

			if !pc.HasRole(r.Context(), libID, userID, required) {
				http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
