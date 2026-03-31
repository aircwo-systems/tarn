package dynamodb

import "fmt"

// ServiceError is returned for AWS-compatible DynamoDB API failures.
type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *ServiceError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ServiceError) StatusCode() int {
	if e == nil || e.HTTPStatus == 0 {
		return 400
	}
	return e.HTTPStatus
}

func validationError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ValidationException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func notFoundError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ResourceNotFoundException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func conditionalCheckFailed(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ConditionalCheckFailedException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func internalError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "InternalServerError", Message: fmt.Sprintf(format, args...), HTTPStatus: 500}
}
