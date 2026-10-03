package platform

import (
	"encoding/json"
	"net/http"
)

type okBody[T any] struct {
	OK   bool `json:"ok"`
	Body T    `json:"data"`
}

type failureBody struct {
	OK    bool         `json:"ok"`
	Error *DomainError `json:"error"`
}

func StatusOf(code Code) int {
	switch code {
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodeForbidden:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeValidation:
		return http.StatusUnprocessableEntity
	case CodeConflict:
		return http.StatusConflict
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeTooMany:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func WriteResult[T any](w http.ResponseWriter, outcome Result[T]) error {
	w.Header().Set("Content-Type", "application/json")
	if !outcome.OK {
		status := http.StatusInternalServerError
		if outcome.Error != nil {
			status = StatusOf(outcome.Error.Code)
		}
		w.WriteHeader(status)
		return json.NewEncoder(w).Encode(failureBody{Error: outcome.Error})
	}
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(okBody[T]{OK: true, Body: outcome.Value})
}

func WriteStatus[T any](w http.ResponseWriter, status int, outcome Result[T]) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if !outcome.OK {
		return json.NewEncoder(w).Encode(failureBody{Error: outcome.Error})
	}
	return json.NewEncoder(w).Encode(okBody[T]{OK: true, Body: outcome.Value})
}
