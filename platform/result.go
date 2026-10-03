package platform

type Code string

const (
	CodeUnauthenticated Code = "UNAUTHENTICATED"
	CodeForbidden       Code = "FORBIDDEN"
	CodeNotFound        Code = "NOT_FOUND"
	CodeValidation      Code = "VALIDATION_FAILED"
	CodeConflict        Code = "CONFLICT"
	CodeUnavailable     Code = "UNAVAILABLE"
	CodeTooMany         Code = "TOO_MANY_REQUESTS"
)

type DomainError struct {
	Code    Code                `json:"code"`
	Message string              `json:"message"`
	Fields  map[string][]string `json:"fieldErrors,omitempty"`
}

type Result[T any] struct {
	OK    bool
	Value T
	Error *DomainError
}

func Succeed[T any](value T) Result[T] {
	return Result[T]{OK: true, Value: value}
}

func Fail[T any](code Code, message string) Result[T] {
	return Result[T]{Error: &DomainError{Code: code, Message: message}}
}

func FailFields[T any](message string, fields map[string][]string) Result[T] {
	return Result[T]{Error: &DomainError{Code: CodeValidation, Message: message, Fields: fields}}
}
