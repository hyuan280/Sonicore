package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoleWeight(t *testing.T) {
	tests := []struct {
		role string
		want int
	}{
		{RoleOwner, 4},
		{RoleAdmin, 3},
		{RoleContributor, 2},
		{RoleViewer, 1},
		{"", 0},
		{"unknown", 0},
		{"OWNER", 0},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			assert.Equal(t, tt.want, roleWeight(tt.role))
		})
	}
}
