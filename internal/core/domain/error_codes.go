package domain

// ErrorCode is a numeric error code returned in API responses alongside the
// human-readable message. The frontend uses the code for i18n translation.
//
// Ranges are reserved by category for forward compatibility:
//
//	  1 –  99  common
//	100 – 199  auth
//	200 – 299  library
//	300 – 399  user
//	400 – 499  metadata
//	500 – 599  jukebox
//	600 – 699  stream
//	700 – 799  platform
//	800 – 899  admin
//	900 – 999  download
type ErrorCode int

// ---- common (1-99) ----

const (
	ErrUnauthorized      ErrorCode = 1
	ErrForbidden         ErrorCode = 2
	ErrNotFound          ErrorCode = 3
	ErrInvalidBody       ErrorCode = 4
	ErrInternal          ErrorCode = 5
	ErrTooManyRequests   ErrorCode = 6
	ErrMissingAuthHeader ErrorCode = 7
	ErrInvalidAuthFormat ErrorCode = 8
	ErrInvalidToken      ErrorCode = 9
)

// ---- auth (100-199) ----

const (
	ErrAuthCredentialsRequired  ErrorCode = 100
	ErrAuthInvalidCredentials   ErrorCode = 101
	ErrAuthRegistrationDisabled ErrorCode = 102
	ErrAuthUsernameExists       ErrorCode = 103
	ErrAuthEmailExists          ErrorCode = 104
	ErrAuthPasswordTooShort     ErrorCode = 105
	ErrAuthRefreshRequired      ErrorCode = 106
	ErrAuthInvalidRefresh       ErrorCode = 107
	ErrAuthHashPassword         ErrorCode = 108
	ErrAuthCreateUser           ErrorCode = 109
	ErrAuthUserNotFound         ErrorCode = 110
	ErrAuthRevokeTokens         ErrorCode = 111
	ErrAuthGenerateToken        ErrorCode = 112
	ErrAuthStoreToken           ErrorCode = 113
	ErrAuthGenerateSession      ErrorCode = 114
	ErrAuthInsufficientPerms    ErrorCode = 115
	ErrAuthRegistrationFields   ErrorCode = 116
)

// ---- library (200-299) ----

const (
	ErrLibNameRequired       ErrorCode = 200
	ErrLibInvalidStorageMode ErrorCode = 201
	ErrLibCreateFailed       ErrorCode = 202
	ErrLibListFailed         ErrorCode = 203
	ErrLibAccessDenied       ErrorCode = 204
	ErrLibNotOwner           ErrorCode = 205
	ErrLibNeedContributor    ErrorCode = 206
	ErrLibInvalidRole        ErrorCode = 207
	ErrLibAddMemberFailed    ErrorCode = 208
	ErrLibCannotRemoveOwner  ErrorCode = 209
	ErrLibRemoveMemberFailed ErrorCode = 210
	ErrLibChangeOwnerRole    ErrorCode = 211
	ErrLibUpdateRoleFailed   ErrorCode = 212
	ErrLibListMembersFailed  ErrorCode = 213
	ErrLibDeleteFailed       ErrorCode = 214
)

// ---- user (300-399) ----

const (
	ErrUserNotFound         ErrorCode = 300
	ErrUserWrongPassword    ErrorCode = 301
	ErrUserHashFailed       ErrorCode = 302
	ErrUserUpdatePassword   ErrorCode = 303
	ErrUserClientMismatch   ErrorCode = 304
	ErrUserSessionGenerate  ErrorCode = 305
	ErrUserQueryFailed      ErrorCode = 306
	ErrUserPlaylistNotFound ErrorCode = 307
	ErrUserTrackIDRequired  ErrorCode = 308
	ErrUserTrackIDsRequired ErrorCode = 309
	ErrUserTrackNotFound    ErrorCode = 310
)

// ---- metadata (400-499) ----

const (
	ErrMetaFileHashRequired     ErrorCode = 400
	ErrMetaTitleRequired        ErrorCode = 401
	ErrMetaInvalidRequest       ErrorCode = 402
	ErrMetaTrackNotFound        ErrorCode = 403
	ErrMetaUpdateTrack          ErrorCode = 404
	ErrMetaProbeFile            ErrorCode = 405
	ErrMetaNameRequired         ErrorCode = 406
	ErrMetaSaveAlbums           ErrorCode = 407
	ErrMetaUnsupportedSource    ErrorCode = 408
	ErrMetaLookupEnrichment     ErrorCode = 409
	ErrMetaIdentifyTrack        ErrorCode = 410
	ErrMetaUpdateArtists        ErrorCode = 411
	ErrMetaUpdateAlbum          ErrorCode = 412
	ErrMetaDeleteCovers         ErrorCode = 413
	ErrMetaSourceRequired       ErrorCode = 414
	ErrMetaNotFoundInSource     ErrorCode = 415
	ErrMetaUpdateAlbumYearGenre ErrorCode = 416
	ErrLyricsTrackIDRequired    ErrorCode = 417
	ErrLyricsUpdateOffsetFailed ErrorCode = 418
)

// ---- jukebox (500-599) ----

const (
	ErrJbxNameRequired      ErrorCode = 500
	ErrJbxDeviceNotFound    ErrorCode = 501
	ErrJbxDeviceBound       ErrorCode = 502
	ErrJbxCreateFailed      ErrorCode = 503
	ErrJbxNotFound          ErrorCode = 504
	ErrJbxUpdateFailed      ErrorCode = 505
	ErrJbxDeleteFailed      ErrorCode = 506
	ErrJbxDeviceIDRequired  ErrorCode = 507
	ErrJbxDeviceIsBound     ErrorCode = 508
	ErrJbxListDevicesFailed ErrorCode = 509
	ErrJbxMethodNotAllowed  ErrorCode = 510
)

// ---- stream (600-699) ----

const (
	ErrStreamMissingSession   ErrorCode = 600
	ErrStreamInvalidSession   ErrorCode = 601
	ErrStreamTrackNotFound    ErrorCode = 602
	ErrStreamForbidden        ErrorCode = 603
	ErrStreamTranscodeError   ErrorCode = 604
	ErrStreamTranscodeTimeout ErrorCode = 605
	ErrStreamMSEUnavailable   ErrorCode = 606
	ErrStreamOutOfRange       ErrorCode = 607
)

// ---- platform (700-799) ----

const (
	ErrPlatUnknownPlatform ErrorCode = 700
	ErrPlatInvalidID       ErrorCode = 701
	ErrPlatUnsupportedType ErrorCode = 702
	ErrPlatUpstream        ErrorCode = 703
)

// ---- admin (800-899) ----

const (
	ErrAdminListUsers        ErrorCode = 800
	ErrAdminInvalidRole      ErrorCode = 801
	ErrAdminActorNotFound    ErrorCode = 802
	ErrAdminTargetNotFound   ErrorCode = 803
	ErrAdminChangeSuperAdmin ErrorCode = 804
	ErrAdminManageAdmin      ErrorCode = 805
	ErrAdminUpdateRole       ErrorCode = 806
	ErrAdminCookieConflict   ErrorCode = 807
	ErrAdminStoreCookie      ErrorCode = 808
	ErrAdminInvalidLogLevel  ErrorCode = 809
	ErrAdminSaveSettings     ErrorCode = 810
	ErrAdminReadDirectory    ErrorCode = 811
	ErrAdminAccessRequired   ErrorCode = 812
)

// ---- download (900-999) ----

const (
	ErrDownloadURLRequired ErrorCode = 900
	ErrDownloadJobNotFound ErrorCode = 901
)

// Category returns the category label for the error code's range.
func (c ErrorCode) Category() string {
	switch {
	case c >= 1 && c <= 99:
		return "common"
	case c >= 100 && c <= 199:
		return "auth"
	case c >= 200 && c <= 299:
		return "library"
	case c >= 300 && c <= 399:
		return "user"
	case c >= 400 && c <= 499:
		return "metadata"
	case c >= 500 && c <= 599:
		return "jukebox"
	case c >= 600 && c <= 699:
		return "stream"
	case c >= 700 && c <= 799:
		return "platform"
	case c >= 800 && c <= 899:
		return "admin"
	case c >= 900 && c <= 999:
		return "download"
	default:
		return "common"
	}
}

// Key returns a snake_case key suitable for i18n lookup, e.g. "INVALID_CREDENTIALS".
// The key mirrors the TypeScript errorCodes.ts const name.
func (c ErrorCode) Key() string {
	if key, ok := errorCodeKeys[c]; ok {
		return key
	}
	return "INTERNAL_ERROR"
}

// DefaultMessage returns the English fallback string for the error code.
func (c ErrorCode) DefaultMessage() string {
	if msg, ok := errorCodeMessages[c]; ok {
		return msg
	}
	return "An unexpected error occurred"
}

// errorCodeKeys maps each code to its i18n key (matches TypeScript export name).
var errorCodeKeys = map[ErrorCode]string{
	ErrUnauthorized:             "UNAUTHORIZED",
	ErrForbidden:                "FORBIDDEN",
	ErrNotFound:                 "NOT_FOUND",
	ErrInvalidBody:              "INVALID_BODY",
	ErrInternal:                 "INTERNAL_ERROR",
	ErrTooManyRequests:          "TOO_MANY_REQUESTS",
	ErrMissingAuthHeader:        "MISSING_AUTH_HEADER",
	ErrInvalidAuthFormat:        "INVALID_AUTH_FORMAT",
	ErrInvalidToken:             "INVALID_TOKEN",
	ErrAuthCredentialsRequired:  "CREDENTIALS_REQUIRED",
	ErrAuthInvalidCredentials:   "INVALID_CREDENTIALS",
	ErrAuthRegistrationDisabled: "REGISTRATION_DISABLED",
	ErrAuthUsernameExists:       "USERNAME_EXISTS",
	ErrAuthEmailExists:          "EMAIL_EXISTS",
	ErrAuthPasswordTooShort:     "PASSWORD_TOO_SHORT",
	ErrAuthRefreshRequired:      "REFRESH_REQUIRED",
	ErrAuthInvalidRefresh:       "INVALID_REFRESH",
	ErrAuthHashPassword:         "HASH_PASSWORD_FAILED",
	ErrAuthCreateUser:           "CREATE_USER_FAILED",
	ErrAuthUserNotFound:         "AUTH_USER_NOT_FOUND",
	ErrAuthRevokeTokens:         "REVOKE_TOKENS_FAILED",
	ErrAuthGenerateToken:        "GENERATE_TOKEN_FAILED",
	ErrAuthStoreToken:           "STORE_TOKEN_FAILED",
	ErrAuthGenerateSession:      "GENERATE_SESSION_FAILED",
	ErrAuthInsufficientPerms:    "INSUFFICIENT_PERMISSIONS",
	ErrAuthRegistrationFields:   "REGISTRATION_FIELDS_REQUIRED",
	ErrLibNameRequired:          "NAME_REQUIRED",
	ErrLibInvalidStorageMode:    "INVALID_STORAGE_MODE",
	ErrLibCreateFailed:          "CREATE_LIBRARY_FAILED",
	ErrLibListFailed:            "LIST_LIBRARIES_FAILED",
	ErrLibAccessDenied:          "ACCESS_DENIED",
	ErrLibNotOwner:              "NOT_OWNER",
	ErrLibNeedContributor:       "NEED_CONTRIBUTOR_ROLE",
	ErrLibInvalidRole:           "INVALID_ROLE",
	ErrLibAddMemberFailed:       "ADD_MEMBER_FAILED",
	ErrLibCannotRemoveOwner:     "CANNOT_REMOVE_OWNER",
	ErrLibRemoveMemberFailed:    "REMOVE_MEMBER_FAILED",
	ErrLibChangeOwnerRole:       "CANNOT_CHANGE_OWNER_ROLE",
	ErrLibUpdateRoleFailed:      "UPDATE_ROLE_FAILED",
	ErrLibListMembersFailed:     "LIST_MEMBERS_FAILED",
	ErrUserNotFound:             "USER_NOT_FOUND",
	ErrUserWrongPassword:        "WRONG_PASSWORD",
	ErrUserHashFailed:           "HASH_FAILED",
	ErrUserUpdatePassword:       "UPDATE_PASSWORD_FAILED",
	ErrUserClientMismatch:       "CLIENT_MISMATCH",
	ErrUserSessionGenerate:      "SESSION_GENERATE_FAILED",
	ErrUserQueryFailed:          "QUERY_FAILED",
	ErrUserPlaylistNotFound:     "PLAYLIST_NOT_FOUND",
	ErrUserTrackIDRequired:      "TRACK_ID_REQUIRED",
	ErrUserTrackIDsRequired:     "TRACK_IDS_REQUIRED",
	ErrUserTrackNotFound:        "TRACK_NOT_FOUND",
	ErrMetaFileHashRequired:     "FILE_HASH_REQUIRED",
	ErrMetaTitleRequired:        "TITLE_REQUIRED",
	ErrMetaInvalidRequest:       "INVALID_REQUEST",
	ErrMetaTrackNotFound:        "META_TRACK_NOT_FOUND",
	ErrMetaUpdateTrack:          "UPDATE_TRACK_FAILED",
	ErrMetaProbeFile:            "PROBE_FILE_FAILED",
	ErrMetaNameRequired:         "META_NAME_REQUIRED",
	ErrMetaSaveAlbums:           "SAVE_ALBUMS_FAILED",
	ErrJbxNameRequired:          "JBX_NAME_REQUIRED",
	ErrJbxDeviceNotFound:        "DEVICE_NOT_FOUND",
	ErrJbxDeviceBound:           "DEVICE_ALREADY_BOUND",
	ErrJbxCreateFailed:          "CREATE_JUKEBOX_FAILED",
	ErrJbxNotFound:              "JUKEBOX_NOT_FOUND",
	ErrJbxUpdateFailed:          "UPDATE_JUKEBOX_FAILED",
	ErrJbxDeleteFailed:          "DELETE_JUKEBOX_FAILED",
	ErrJbxDeviceIDRequired:      "DEVICE_ID_REQUIRED",
	ErrJbxDeviceIsBound:         "DEVICE_IS_BOUND",
	ErrJbxListDevicesFailed:     "LIST_DEVICES_FAILED",
	ErrJbxMethodNotAllowed:      "METHOD_NOT_ALLOWED",
	ErrStreamMissingSession:     "MISSING_SESSION",
	ErrStreamInvalidSession:     "INVALID_SESSION",
	ErrStreamTrackNotFound:      "STREAM_TRACK_NOT_FOUND",
	ErrStreamForbidden:          "STREAM_FORBIDDEN",
	ErrStreamTranscodeError:     "TRANSCODE_ERROR",
	ErrStreamTranscodeTimeout:   "TRANSCODE_TIMEOUT",
	ErrStreamMSEUnavailable:     "MSE_UNAVAILABLE",
	ErrStreamOutOfRange:         "OUT_OF_RANGE",
	ErrPlatUnknownPlatform:      "UNKNOWN_PLATFORM",
	ErrPlatInvalidID:            "INVALID_PLATFORM_ID",
	ErrPlatUnsupportedType:      "UNSUPPORTED_SEARCH_TYPE",
	ErrPlatUpstream:             "PLATFORM_UPSTREAM_ERROR",
	ErrLibDeleteFailed:          "DELETE_LIBRARY_FAILED",
	ErrMetaUnsupportedSource:    "UNSUPPORTED_METADATA_SOURCE",
	ErrMetaLookupEnrichment:     "LOOKUP_ENRICHMENT_FAILED",
	ErrMetaIdentifyTrack:        "IDENTIFY_TRACK_FAILED",
	ErrMetaUpdateArtists:        "UPDATE_ARTISTS_FAILED",
	ErrMetaUpdateAlbum:          "UPDATE_ALBUM_FAILED",
	ErrMetaDeleteCovers:         "DELETE_COVERS_FAILED",
	ErrMetaSourceRequired:       "SOURCE_REQUIRED",
	ErrMetaNotFoundInSource:     "NOT_FOUND_IN_SOURCE",
	ErrMetaUpdateAlbumYearGenre: "UPDATE_ALBUM_YEAR_GENRE_FAILED",
	ErrLyricsTrackIDRequired:    "LYRICS_TRACK_ID_REQUIRED",
	ErrLyricsUpdateOffsetFailed: "LYRICS_UPDATE_OFFSET_FAILED",
	ErrAdminListUsers:           "ADMIN_LIST_USERS_FAILED",
	ErrAdminInvalidRole:         "ADMIN_INVALID_ROLE",
	ErrAdminActorNotFound:       "ADMIN_ACTOR_NOT_FOUND",
	ErrAdminTargetNotFound:      "ADMIN_TARGET_NOT_FOUND",
	ErrAdminChangeSuperAdmin:    "ADMIN_CHANGE_SUPER_ADMIN",
	ErrAdminManageAdmin:         "ADMIN_MANAGE_ADMIN",
	ErrAdminUpdateRole:          "ADMIN_UPDATE_ROLE_FAILED",
	ErrAdminCookieConflict:      "ADMIN_COOKIE_CONFLICT",
	ErrAdminStoreCookie:         "ADMIN_STORE_COOKIE_FAILED",
	ErrAdminInvalidLogLevel:     "ADMIN_INVALID_LOG_LEVEL",
	ErrAdminSaveSettings:        "ADMIN_SAVE_SETTINGS_FAILED",
	ErrAdminReadDirectory:       "ADMIN_READ_DIRECTORY_FAILED",
	ErrAdminAccessRequired:      "ADMIN_ACCESS_REQUIRED",
	ErrDownloadURLRequired:      "DOWNLOAD_URL_REQUIRED",
	ErrDownloadJobNotFound:      "DOWNLOAD_JOB_NOT_FOUND",
}

// errorCodeMessages maps each code to its default English fallback message.
var errorCodeMessages = map[ErrorCode]string{
	ErrUnauthorized:             "Unauthorized",
	ErrForbidden:                "Forbidden",
	ErrNotFound:                 "Not found",
	ErrInvalidBody:              "Invalid request body",
	ErrInternal:                 "Internal server error",
	ErrTooManyRequests:          "Too many requests",
	ErrMissingAuthHeader:        "Missing authorization header",
	ErrInvalidAuthFormat:        "Invalid authorization format",
	ErrInvalidToken:             "Invalid or expired token",
	ErrAuthCredentialsRequired:  "Username and password are required",
	ErrAuthInvalidCredentials:   "Invalid username or password",
	ErrAuthRegistrationDisabled: "Registration is disabled",
	ErrAuthUsernameExists:       "Username already exists",
	ErrAuthEmailExists:          "Email already exists",
	ErrAuthPasswordTooShort:     "Password must be at least 6 characters",
	ErrAuthRefreshRequired:      "Refresh token is required",
	ErrAuthInvalidRefresh:       "Invalid or expired refresh token",
	ErrAuthHashPassword:         "Failed to hash password",
	ErrAuthCreateUser:           "Failed to create user",
	ErrAuthUserNotFound:         "User not found",
	ErrAuthRevokeTokens:         "Failed to revoke tokens",
	ErrAuthGenerateToken:        "Failed to generate token",
	ErrAuthStoreToken:           "Failed to store refresh token",
	ErrAuthGenerateSession:      "Failed to generate session",
	ErrAuthInsufficientPerms:    "Insufficient permissions",
	ErrAuthRegistrationFields:   "Username, email, and password are required",
	ErrLibNameRequired:          "Name is required",
	ErrLibInvalidStorageMode:    "Invalid metadata storage mode",
	ErrLibCreateFailed:          "Failed to create library",
	ErrLibListFailed:            "Failed to list libraries",
	ErrLibAccessDenied:          "Access denied",
	ErrLibNotOwner:              "Only the owner can perform this action",
	ErrLibNeedContributor:       "Need contributor role or higher",
	ErrLibInvalidRole:           "Invalid role",
	ErrLibAddMemberFailed:       "Failed to add member",
	ErrLibCannotRemoveOwner:     "Cannot remove the owner",
	ErrLibRemoveMemberFailed:    "Failed to remove member",
	ErrLibChangeOwnerRole:       "Cannot change the owner's role",
	ErrLibUpdateRoleFailed:      "Failed to update role",
	ErrLibListMembersFailed:     "Failed to list members",
	ErrUserNotFound:             "User not found",
	ErrUserWrongPassword:        "Wrong password",
	ErrUserHashFailed:           "Failed to hash password",
	ErrUserUpdatePassword:       "Failed to update password",
	ErrUserClientMismatch:       "Client mismatch, please re-login",
	ErrUserSessionGenerate:      "Failed to generate session",
	ErrUserQueryFailed:          "Query failed",
	ErrUserPlaylistNotFound:     "Playlist not found",
	ErrUserTrackIDRequired:      "Track ID is required",
	ErrUserTrackIDsRequired:     "Track IDs are required",
	ErrUserTrackNotFound:        "Track not found",
	ErrMetaFileHashRequired:     "File hash is required",
	ErrMetaTitleRequired:        "Title is required",
	ErrMetaInvalidRequest:       "Invalid request",
	ErrMetaTrackNotFound:        "Track not found",
	ErrMetaUpdateTrack:          "Failed to update track",
	ErrMetaProbeFile:            "Failed to probe file",
	ErrMetaNameRequired:         "Name is required",
	ErrMetaSaveAlbums:           "Failed to save albums",
	ErrJbxNameRequired:          "Name is required",
	ErrJbxDeviceNotFound:        "Device config not found",
	ErrJbxDeviceBound:           "Device already bound to another jukebox",
	ErrJbxCreateFailed:          "Failed to create jukebox",
	ErrJbxNotFound:              "Jukebox not found",
	ErrJbxUpdateFailed:          "Failed to update jukebox",
	ErrJbxDeleteFailed:          "Failed to delete jukebox",
	ErrJbxDeviceIDRequired:      "Device ID is required",
	ErrJbxDeviceIsBound:         "Device is bound to a jukebox",
	ErrJbxListDevicesFailed:     "Failed to list devices",
	ErrJbxMethodNotAllowed:      "Method not allowed",
	ErrStreamMissingSession:     "Missing session",
	ErrStreamInvalidSession:     "Invalid session",
	ErrStreamTrackNotFound:      "Track not found",
	ErrStreamForbidden:          "Forbidden",
	ErrStreamTranscodeError:     "Transcoding error",
	ErrStreamTranscodeTimeout:   "Transcode timeout",
	ErrStreamMSEUnavailable:     "MSE unavailable",
	ErrStreamOutOfRange:         "Out of range",
	ErrPlatUnknownPlatform:      "Unknown platform",
	ErrPlatInvalidID:            "Invalid resource id",
	ErrPlatUnsupportedType:      "Unsupported search type",
	ErrPlatUpstream:             "Upstream platform error",
	ErrLibDeleteFailed:          "Failed to delete library",
	ErrMetaUnsupportedSource:    "Unsupported metadata source",
	ErrMetaLookupEnrichment:     "Failed to lookup enrichment data",
	ErrMetaIdentifyTrack:        "Failed to identify track",
	ErrMetaUpdateArtists:        "Failed to update artists",
	ErrMetaUpdateAlbum:          "Failed to update album",
	ErrMetaDeleteCovers:         "Failed to delete old covers",
	ErrMetaSourceRequired:       "Source is required when external_id is provided",
	ErrMetaNotFoundInSource:     "Not found in source",
	ErrMetaUpdateAlbumYearGenre: "Failed to update album year/genre",
	ErrLyricsTrackIDRequired:    "Track ID is required",
	ErrLyricsUpdateOffsetFailed: "Failed to update lyrics offset",
	ErrAdminListUsers:           "Failed to list users",
	ErrAdminInvalidRole:         "Invalid role, must be admin or user",
	ErrAdminActorNotFound:       "Actor not found",
	ErrAdminTargetNotFound:      "Target user not found",
	ErrAdminChangeSuperAdmin:    "Cannot change super admin role",
	ErrAdminManageAdmin:         "Admins cannot manage other admins",
	ErrAdminUpdateRole:          "Failed to update role",
	ErrAdminCookieConflict:      "Cannot set and clear the cookie in one request",
	ErrAdminStoreCookie:         "Failed to store cookie",
	ErrAdminInvalidLogLevel:     "Invalid log level, must be one of: debug, info, warn, warning, error",
	ErrAdminSaveSettings:        "Failed to save settings",
	ErrAdminReadDirectory:       "Failed to read directory",
	ErrAdminAccessRequired:      "Admin access required",
	ErrDownloadURLRequired:      "URL is required",
	ErrDownloadJobNotFound:      "Download job not found",
}
