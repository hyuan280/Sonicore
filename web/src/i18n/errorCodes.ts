// Error codes mirror internal/core/domain/error_codes.go
// Keep both files in sync when adding new codes.

import type { TFunction } from "i18next";

// Category ranges:
//   1 –  99  common
// 100 – 199  auth
// 200 – 299  library
// 300 – 399  user
// 400 – 499  metadata
// 500 – 599  jukebox
// 600 – 699  stream
// 700 – 799  platform

export const ERROR_CODES = {
  // ---- common (1-99) ----
  UNAUTHORIZED: 1,
  FORBIDDEN: 2,
  NOT_FOUND: 3,
  INVALID_BODY: 4,
  INTERNAL_ERROR: 5,
  TOO_MANY_REQUESTS: 6,
  MISSING_AUTH_HEADER: 7,
  INVALID_AUTH_FORMAT: 8,
  INVALID_TOKEN: 9,

  // ---- auth (100-199) ----
  CREDENTIALS_REQUIRED: 100,
  INVALID_CREDENTIALS: 101,
  REGISTRATION_DISABLED: 102,
  USERNAME_EXISTS: 103,
  EMAIL_EXISTS: 104,
  PASSWORD_TOO_SHORT: 105,
  REFRESH_REQUIRED: 106,
  INVALID_REFRESH: 107,
  HASH_PASSWORD_FAILED: 108,
  CREATE_USER_FAILED: 109,
  AUTH_USER_NOT_FOUND: 110,
  REVOKE_TOKENS_FAILED: 111,
  GENERATE_TOKEN_FAILED: 112,
  STORE_TOKEN_FAILED: 113,
  GENERATE_SESSION_FAILED: 114,
  INSUFFICIENT_PERMISSIONS: 115,
  REGISTRATION_FIELDS_REQUIRED: 116,

  // ---- library (200-299) ----
  NAME_REQUIRED: 200,
  INVALID_STORAGE_MODE: 201,
  CREATE_LIBRARY_FAILED: 202,
  LIST_LIBRARIES_FAILED: 203,
  ACCESS_DENIED: 204,
  NOT_OWNER: 205,
  NEED_CONTRIBUTOR_ROLE: 206,
  INVALID_ROLE: 207,
  ADD_MEMBER_FAILED: 208,
  CANNOT_REMOVE_OWNER: 209,
  REMOVE_MEMBER_FAILED: 210,
  CANNOT_CHANGE_OWNER_ROLE: 211,
  UPDATE_ROLE_FAILED: 212,
  LIST_MEMBERS_FAILED: 213,

  // ---- user (300-399) ----
  USER_NOT_FOUND: 300,
  WRONG_PASSWORD: 301,
  HASH_FAILED: 302,
  UPDATE_PASSWORD_FAILED: 303,
  CLIENT_MISMATCH: 304,
  SESSION_GENERATE_FAILED: 305,
  QUERY_FAILED: 306,
  PLAYLIST_NOT_FOUND: 307,
  TRACK_ID_REQUIRED: 308,
  TRACK_IDS_REQUIRED: 309,
  TRACK_NOT_FOUND: 310,

  // ---- metadata (400-499) ----
  FILE_HASH_REQUIRED: 400,
  TITLE_REQUIRED: 401,
  INVALID_REQUEST: 402,
  META_TRACK_NOT_FOUND: 403,
  UPDATE_TRACK_FAILED: 404,
  PROBE_FILE_FAILED: 405,
  META_NAME_REQUIRED: 406,
  SAVE_ALBUMS_FAILED: 407,

  // ---- jukebox (500-599) ----
  JBX_NAME_REQUIRED: 500,
  DEVICE_NOT_FOUND: 501,
  DEVICE_ALREADY_BOUND: 502,
  CREATE_JUKEBOX_FAILED: 503,
  JUKEBOX_NOT_FOUND: 504,
  UPDATE_JUKEBOX_FAILED: 505,
  DELETE_JUKEBOX_FAILED: 506,
  DEVICE_ID_REQUIRED: 507,
  DEVICE_IS_BOUND: 508,
  LIST_DEVICES_FAILED: 509,
  METHOD_NOT_ALLOWED: 510,

  // ---- stream (600-699) ----
  MISSING_SESSION: 600,
  INVALID_SESSION: 601,
  STREAM_TRACK_NOT_FOUND: 602,
  STREAM_FORBIDDEN: 603,
  TRANSCODE_ERROR: 604,
  TRANSCODE_TIMEOUT: 605,
  MSE_UNAVAILABLE: 606,
  OUT_OF_RANGE: 607,

  // ---- platform (700-799) ----
  UNKNOWN_PLATFORM: 700,
  INVALID_PLATFORM_ID: 701,
  UNSUPPORTED_SEARCH_TYPE: 702,
  PLATFORM_UPSTREAM_ERROR: 703,
} as const;

const CODE_TO_I18N_KEY: Record<number, string> = {};
const CODE_TO_NAME: Record<number, string> = {};
for (const [key, value] of Object.entries(ERROR_CODES)) {
  CODE_TO_I18N_KEY[value] = `errors.${getCategory(value)}.${key}`;
  CODE_TO_NAME[value] = key;
}

function getCategory(code: number): string {
  if (code <= 99) return "common";
  if (code <= 199) return "auth";
  if (code <= 299) return "library";
  if (code <= 399) return "user";
  if (code <= 499) return "metadata";
  if (code <= 599) return "jukebox";
  if (code <= 699) return "stream";
  if (code <= 799) return "platform";
  return "common";
}

export function getErrorI18nKey(code: number): string | undefined {
  return CODE_TO_I18N_KEY[code];
}

export function getErrorName(code: number): string | undefined {
  return CODE_TO_NAME[code];
}

export interface ApiError {
  code?: number;
  error?: string;
  message?: string;
  status?: number;
}

export function translateApiError(t: TFunction, err: unknown): string {
  const apiErr = err as ApiError;
  if (apiErr?.code !== undefined) {
    const key = getErrorI18nKey(apiErr.code);
    if (key) return t(key, { defaultValue: apiErr?.error || apiErr?.message });
  }
  return apiErr?.error || apiErr?.message || t("common.unknown");
}
