package cognito

import "fmt"

// Error is a Cognito API error. Code is the AWS error type written to
// `__type`; every Cognito client error uses HTTP 400.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func invalidParameter(format string, args ...any) *Error {
	return newError("InvalidParameterException", format, args...)
}

func notAuthorized(format string, args ...any) *Error {
	return newError("NotAuthorizedException", format, args...)
}

func resourceNotFound(format string, args ...any) *Error {
	return newError("ResourceNotFoundException", format, args...)
}

func poolNotFound(poolID string) *Error {
	return resourceNotFound("User pool %s does not exist.", poolID)
}

func clientNotFound(clientID string) *Error {
	return resourceNotFound("User pool client %s does not exist.", clientID)
}

var (
	errUserNotFound        = &Error{"UserNotFoundException", "User does not exist."}
	errIncorrectPassword   = &Error{"NotAuthorizedException", "Incorrect username or password."}
	errUserDisabled        = &Error{"NotAuthorizedException", "User is disabled."}
	errUserNotConfirmed    = &Error{"UserNotConfirmedException", "User is not confirmed."}
	errResetRequired       = &Error{"PasswordResetRequiredException", "Password reset required for the user"}
	errCodeMismatch        = &Error{"CodeMismatchException", "Invalid verification code provided, please try again."}
	errExpiredCode         = &Error{"ExpiredCodeException", "Invalid code provided, please request a code again."}
	errAttemptLimit        = &Error{"LimitExceededException", "Attempt limit exceeded, please try after some time."}
	errInvalidAccessToken  = &Error{"NotAuthorizedException", "Invalid Access Token"}
	errAccessTokenExpired  = &Error{"NotAuthorizedException", "Access Token has expired"}
	errAccessTokenRevoked  = &Error{"NotAuthorizedException", "Access Token has been revoked"}
	errInvalidRefreshToken = &Error{"NotAuthorizedException", "Invalid Refresh Token"}
	errRefreshTokenRevoked = &Error{"NotAuthorizedException", "Refresh Token has been revoked"}
	errInvalidSession      = &Error{"NotAuthorizedException", "Invalid session for the user, session is expired."}
	errTempPasswordExpired = &Error{"NotAuthorizedException", "Temporary password has expired and must be reset by an administrator."}
)

func secretHashError(clientID string) *Error {
	return notAuthorized("Unable to verify secret hash for client %s", clientID)
}

func flowNotEnabled(flow string) *Error {
	return invalidParameter("%s flow not enabled for this client", flow)
}

// NotImplemented reports an operation Tarn does not emulate.
func NotImplemented(action string) *Error {
	return newError("NotImplementedException", "Tarn does not implement Cognito %s", action)
}
