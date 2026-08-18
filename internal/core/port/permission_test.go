package port

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/sonicore/server/internal/core/domain"
)

func TestHasPermissionSuperAdmin(t *testing.T) {
	allPerms := []Permission{
		PermAdminAccess,
		PermAdminManageUsers,
		PermAdminManageSettings,
		PermLibraryCreate,
		PermLibraryDelete,
		PermLibraryManageMembers,
		PermLibraryScan,
		PermMetadataEdit,
		PermDownload,
	}
	for _, perm := range allPerms {
		t.Run(string(perm), func(t *testing.T) {
			assert.True(t, HasPermission(domain.RoleSuperAdmin, perm))
		})
	}
}

func TestHasPermissionAdmin(t *testing.T) {
	allowed := []Permission{
		PermAdminAccess,
		PermAdminManageUsers,
		PermLibraryCreate,
		PermLibraryDelete,
		PermLibraryManageMembers,
		PermLibraryScan,
		PermMetadataEdit,
		PermDownload,
	}
	denied := []Permission{
		PermAdminManageSettings,
	}

	for _, perm := range allowed {
		t.Run("allowed "+string(perm), func(t *testing.T) {
			assert.True(t, HasPermission(domain.RoleAdmin, perm))
		})
	}
	for _, perm := range denied {
		t.Run("denied "+string(perm), func(t *testing.T) {
			assert.False(t, HasPermission(domain.RoleAdmin, perm))
		})
	}
}

func TestHasPermissionUser(t *testing.T) {
	for _, perm := range []Permission{
		PermAdminAccess,
		PermAdminManageUsers,
		PermAdminManageSettings,
		PermLibraryCreate,
		PermLibraryDelete,
		PermLibraryManageMembers,
		PermLibraryScan,
		PermMetadataEdit,
		PermDownload,
	} {
		t.Run(string(perm), func(t *testing.T) {
			assert.False(t, HasPermission(domain.RoleUser, perm))
		})
	}
}

func TestHasPermissionUnknownRole(t *testing.T) {
	assert.False(t, HasPermission(domain.Role("hacker"), PermDownload))
	assert.False(t, HasPermission(domain.Role(""), PermAdminAccess))
}

func TestHasPermissionUnknownPermission(t *testing.T) {
	assert.False(t, HasPermission(domain.RoleSuperAdmin, Permission("nonexistent:perm")))
}
