package httpresponse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Response[T any] struct {
	Status int    `json:"Status"`
	Body   any    `json:"Body,omitempty"`
	Code   string `json:"Code,omitempty"`
}

type ErrorResponse struct {
	ErrorDescription string `json:"ErrorDescription"`
}

const INTERNALERRORJSON = "{\"statuses\": 500,\"body\":{\"error\": \"Internal server error\"}}"

const MALFORMEDJSON_errorDesc = "json unmarshalling error"

func WriteResponseWithStatus(w http.ResponseWriter, status int, code string, body any) {
	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(status)

	resp := Response[any]{Status: status}
	if code != "" {
		resp.Code = code
	}
	if body != nil {
		resp.Body = body
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		WriteInternalErrorResponse(w)
	}
}

func marshalStatusJson(status int, code string, body any) ([]byte, error) {
	response := Response[any]{
		Status: status,
		Body:   body,
		Code:   code,
	}
	marshal, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}
	return marshal, nil
}

func WriteInternalErrorResponse(w http.ResponseWriter) {
	// := slog.With("requestID", ctx.Value("traceID"))
	// implementation similar to http.Error, only difference is the Content-type
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(500)
	_, _ = fmt.Fprintln(w, INTERNALERRORJSON)
	//logger.Info("response internal error", "body", INTERNALERRORJSON)
}

type APIError struct {
	Message string `json:"message"`
}

func WriteAPIError(w http.ResponseWriter, status int, code, msg string) {
	WriteResponseWithStatus(w, status, code, APIError{Message: msg})
}
