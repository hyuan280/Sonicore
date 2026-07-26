package port

import "github.com/sonicore/server/internal/core/domain"

type Permission string

const (
	PermAdminAccess         Permission = "admin:access"
	PermAdminManageUsers    Permission = "admin:users:manage"
	PermAdminManageSettings Permission = "admin:settings:manage"
	PermLibraryCreate       Permission = "library:create"
	PermLibraryDelete       Permission = "library:delete"
	PermLibraryManageMembers Permission = "library:members:manage"
	PermLibraryScan         Permission = "library:scan"
	PermMetadataEdit        Permission = "metadata:edit"
	PermDownload            Permission = "download"
)

var rolePermissions = map[domain.Role][]Permission{
	domain.RoleSuperAdmin: {
		PermAdminAccess,
		PermAdminManageUsers,
		PermAdminManageSettings,
		PermLibraryCreate,
		PermLibraryDelete,
		PermLibraryManageMembers,
		PermLibraryScan,
		PermMetadataEdit,
		PermDownload,
	},
	domain.RoleAdmin: {
		PermAdminAccess,
		PermAdminManageUsers,
		PermLibraryCreate,
		PermLibraryDelete,
		PermLibraryManageMembers,
		PermLibraryScan,
		PermMetadataEdit,
		PermDownload,
	},
	domain.RoleUser: {},
}

func HasPermission(role domain.Role, perm Permission) bool {
	for _, p := range rolePermissions[role] {
		if p == perm {
			return true
		}
	}
	return false
}
