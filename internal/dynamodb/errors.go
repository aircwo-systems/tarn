package dynamodb

import (
	"fmt"
	"strings"
)

// ServiceError is returned for AWS-compatible DynamoDB API failures.
type ServiceError struct {
	Code       string
	Message    string
	HTTPStatus int
	// Item is the existing item, for ReturnValuesOnConditionCheckFailure ALL_OLD.
	Item map[string]any
	// CancellationReasons has one entry per transaction action, in order.
	CancellationReasons []CancellationReason
}

// CancellationReason explains one action's part in a cancelled transaction.
type CancellationReason struct {
	Code    string         `json:"Code"`
	Message string         `json:"Message,omitempty"`
	Item    map[string]any `json:"Item,omitempty"`
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

func resourceInUseError(format string, args ...any) *ServiceError {
	return &ServiceError{Code: "ResourceInUseException", Message: fmt.Sprintf(format, args...), HTTPStatus: 400}
}

func transactionCanceled(reasons []CancellationReason) *ServiceError {
	codes := make([]string, len(reasons))
	for i, r := range reasons {
		codes[i] = r.Code
	}
	return &ServiceError{
		Code:                "TransactionCanceledException",
		Message:             fmt.Sprintf("Transaction cancelled, please refer cancellation reasons for specific reasons [%s]", strings.Join(codes, ", ")),
		HTTPStatus:          400,
		CancellationReasons: reasons,
	}
}
