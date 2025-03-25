package httpresponse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Response[T any] struct {
	Status int `json:"Status"`
	Body   any `json:"Body,omitempty"`
}

type ErrorResponse struct {
	ErrorDescription string `json:"ErrorDescription"`
}

const INTERNALERRORJSON = "{\"status\": 500,\"body\":{\"error\": \"Internal server error\"}}"

const MALFORMEDJSON_errorDesc = "json unmarshalling error"

func WriteResponseWithStatus(w http.ResponseWriter, status int, body any) {
	//logger := slog.With("requestID", ctx.Value("traceID"))
	w.Header().Set("Content-Type", "application/json")
	jsonByte, err := marshalStatusJson(status, body)
	if err != nil {
		WriteInternalErrorResponse(w)
		return
	}
	_, err = w.Write(jsonByte)
	if err != nil {
		WriteInternalErrorResponse(w)
		return
	}
	//logger.Info("response", "status", status, "body", body)
}

func marshalStatusJson(status int, body any) ([]byte, error) {
	response := Response[any]{
		Status: status,
		Body:   body,
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
