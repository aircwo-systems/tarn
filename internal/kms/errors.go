package kms

import "fmt"

// Error is a KMS API error. Code is the AWS error type written to `__type`;
// every KMS client error uses HTTP 400.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func newError(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func validation(format string, args ...any) *Error {
	return newError("ValidationException", format, args...)
}

func notFound(format string, args ...any) *Error {
	return newError("NotFoundException", format, args...)
}

func keyNotFound(id string) *Error {
	return notFound("Key '%s' does not exist", id)
}

func aliasNotFound(arn string) *Error {
	return notFound("Alias %s is not found.", arn)
}

// errInvalidCiphertext carries no message, as in AWS: the blob is either not
// Tarn's, was made by another key, or its encryption context does not match.
var errInvalidCiphertext = &Error{Code: "InvalidCiphertextException"}

var errIncorrectKey = &Error{
	Code:    "IncorrectKeyException",
	Message: "The key ID in the request does not identify a CMK that can perform this operation.",
}

// NotImplemented reports a KMS operation Tarn does not emulate.
func NotImplemented(action string) *Error {
	return newError("UnsupportedOperationException", "Tarn does not implement KMS %s", action)
}
