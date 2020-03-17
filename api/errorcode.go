package api

import (
	"github.com/bitmark-inc/spring-app-api/protomodel"
)

var (
	errorMessageMap = map[int32]string{
		999:  "internal server error",
		1000: "invalid signature",
		1001: "invalid authorization format",
		1002: "authorization expired",
		1003: "his account has been registered or has been taken",
		1004: "invalid parameters",
		1005: "cannot parse request",
		1006: "account not found",
		1007: "API for this client version has been discontinued",
		1008: "the account is under deletion",

		2000: "file source is not supported",
		2001: "invalid archive file",
		2002: "multiple exporting is not allowed",
	}

	errorInternalServer             = errorJSON(999)
	errorInvalidSignature           = errorJSON(1000)
	errorInvalidAuthorizationFormat = errorJSON(1001)
	errorAuthorizationExpired       = errorJSON(1002)
	errorAccountTaken               = errorJSON(1003)
	errorInvalidParameters          = errorJSON(1004)
	errorCannotParseRequest         = errorJSON(1005)
	errorAccountNotFound            = errorJSON(1006)
	errorUnsupportedClientVersion   = errorJSON(1007)
	errorAccountDeleting            = errorJSON(1008)

	errorFileSourceUnsupported         = errorJSON(2000)
	errorInvalidArchiveFile            = errorJSON(2001)
	errorMultipleExportingIsNotAllowed = errorJSON(2002)
)

// errorJSON converts an error code to a standardized error object
func errorJSON(code int32) *protomodel.ErrorResponse {
	var message string
	if msg, ok := errorMessageMap[code]; ok {
		message = msg
	} else {
		message = "unknown"
	}

	return &protomodel.ErrorResponse{
		Error: &protomodel.Error{
			Code:    code,
			Message: message,
		},
	}
}
