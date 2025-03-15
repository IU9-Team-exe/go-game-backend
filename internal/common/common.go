package common

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

var randSrc rand.Source

func init() {
	randSrc = rand.NewSource(time.Now().UnixNano())
}

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

/*
	func WriteErrorMessageJson(ctx context.Context, w http.ResponseWriter, statusCode int, errorMessage string) {
		errorResponse := domain.Error{Error: errorMessage}
		WriteResponseWithStatus(ctx, w, statusCode, errorResponse)
	}
*/
func RandString(n int) string {
	letterBytes := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	letterIdxBits := 6                    // 6 bits to represent a letter index
	letterIdxMask := 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax := 63 / letterIdxBits    // # of letter indices fitting in 63 bits
	sb := strings.Builder{}
	sb.Grow(n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, randSrc.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = randSrc.Int63(), letterIdxMax
		}
		if idx := int(cache & int64(letterIdxMask)); idx < len(letterBytes) {
			sb.WriteByte(letterBytes[idx])
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return sb.String()
}
