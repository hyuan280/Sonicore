package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorCodeCategory(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{"common boundary low", ErrUnauthorized, "common"},
		{"common boundary high", ErrorCode(99), "common"},
		{"auth boundary low", ErrorCode(100), "auth"},
		{"auth boundary high", ErrorCode(199), "auth"},
		{"library boundary low", ErrorCode(200), "library"},
		{"library boundary high", ErrorCode(299), "library"},
		{"user boundary low", ErrorCode(300), "user"},
		{"user boundary high", ErrorCode(399), "user"},
		{"metadata boundary low", ErrorCode(400), "metadata"},
		{"metadata boundary high", ErrorCode(499), "metadata"},
		{"jukebox boundary low", ErrorCode(500), "jukebox"},
		{"jukebox boundary high", ErrorCode(599), "jukebox"},
		{"stream boundary low", ErrorCode(600), "stream"},
		{"stream boundary high", ErrorCode(699), "stream"},
		{"platform boundary low", ErrorCode(700), "platform"},
		{"platform boundary high", ErrorCode(799), "platform"},
		{"below range falls back to common", ErrorCode(0), "common"},
		{"above range falls back to common", ErrorCode(800), "common"},
		{"negative falls back to common", ErrorCode(-1), "common"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code.Category())
		})
	}
}

func TestErrorCodeCategoryForAllKnownCodes(t *testing.T) {
	for code := range errorCodeKeys {
		assert.NotEmpty(t, code.Category(), "code %d should have a category", code)
	}
}

func TestErrorCodeKey(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{"common error", ErrUnauthorized, "UNAUTHORIZED"},
		{"auth error", ErrAuthInvalidCredentials, "INVALID_CREDENTIALS"},
		{"library error", ErrLibNotOwner, "NOT_OWNER"},
		{"user error", ErrUserWrongPassword, "WRONG_PASSWORD"},
		{"metadata error", ErrMetaTitleRequired, "TITLE_REQUIRED"},
		{"jukebox error", ErrJbxNotFound, "JUKEBOX_NOT_FOUND"},
		{"stream error", ErrStreamTranscodeError, "TRANSCODE_ERROR"},
		{"platform error", ErrPlatUpstream, "PLATFORM_UPSTREAM_ERROR"},
		{"unknown code falls back", ErrorCode(9999), "INTERNAL_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code.Key())
		})
	}
}

func TestErrorCodeDefaultMessage(t *testing.T) {
	tests := []struct {
		name string
		code ErrorCode
		want string
	}{
		{"common error", ErrUnauthorized, "Unauthorized"},
		{"auth error", ErrAuthInvalidCredentials, "Invalid username or password"},
		{"library error", ErrLibAccessDenied, "Access denied"},
		{"user error", ErrUserNotFound, "User not found"},
		{"metadata error", ErrMetaProbeFile, "Failed to probe file"},
		{"jukebox error", ErrJbxCreateFailed, "Failed to create jukebox"},
		{"stream error", ErrStreamInvalidSession, "Invalid session"},
		{"platform error", ErrPlatUnknownPlatform, "Unknown platform"},
		{"unknown code falls back", ErrorCode(9999), "An unexpected error occurred"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.code.DefaultMessage())
		})
	}
}

func TestErrorCodeKeyMapCompleteness(t *testing.T) {
	require.NotEmpty(t, errorCodeKeys, "errorCodeKeys map must not be empty")
	for code, key := range errorCodeKeys {
		assert.NotEmpty(t, key, "code %d has empty key", code)
		assert.NotEmpty(t, code.DefaultMessage(), "code %d (%s) missing default message", code, key)
	}
}

func TestErrorCodeMessagesMapCompleteness(t *testing.T) {
	require.NotEmpty(t, errorCodeMessages, "errorCodeMessages map must not be empty")
	for code, msg := range errorCodeMessages {
		assert.NotEmpty(t, msg, "code %d has empty message", code)
		key, ok := errorCodeKeys[code]
		assert.True(t, ok, "code %d has message but no i18n key", code)
		assert.NotEmpty(t, key, "code %d has message but empty key", code)
	}
}
