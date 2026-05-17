package auth0mock

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// APIError wraps a non-2xx response from the /admin0 plane. The mock
// answers errors with the same JSON envelope the real Auth0 API uses
// (statusCode / error / message / errorCode), so APIError carries all
// four fields verbatim and the SDK never invents its own shape.
//
// StatusCode and Reason are always populated — synthesized from the
// HTTP response line when the server's envelope omits them. Message
// and ErrorCode reflect whatever the envelope carried (empty when
// absent). Use errors.As to extract:
//
//	var apiErr *auth0mock.APIError
//	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
//	    // ...
//	}
type APIError struct {
	StatusCode int    `json:"statusCode"`
	Reason     string `json:"error"` // HTTP reason phrase, e.g. "Bad Request".
	Message    string `json:"message"`
	ErrorCode  string `json:"errorCode"`
}

// Error implements the error interface. Always names the status code
// first and includes whichever of (ErrorCode, Message, Reason) the
// envelope carried — falls back to http.StatusText only when every
// envelope field is empty.
func (e *APIError) Error() string {
	switch {
	case e.Message != "" && e.ErrorCode != "":
		return fmt.Sprintf("auth0mock: %d %s: %s", e.StatusCode, e.ErrorCode, e.Message)
	case e.Message != "":
		return fmt.Sprintf("auth0mock: %d %s: %s", e.StatusCode, e.Reason, e.Message)
	case e.ErrorCode != "":
		return fmt.Sprintf("auth0mock: %d %s", e.StatusCode, e.ErrorCode)
	case e.Reason != "":
		return fmt.Sprintf("auth0mock: %d %s", e.StatusCode, e.Reason)
	default:
		return fmt.Sprintf("auth0mock: %d %s", e.StatusCode, http.StatusText(e.StatusCode))
	}
}

// decodeError consumes a non-2xx *http.Response body and returns it as
// an *APIError. Decode is best-effort — if the body parses as JSON the
// SDK keeps every field that landed (errorCode / message / reason),
// even when statusCode is missing from the envelope (some Auth0
// endpoints elide it). Falls back to a synthesized APIError carrying
// the raw body when the body isn't JSON at all.
//
// Always stamps StatusCode from resp.StatusCode if the envelope didn't
// supply one, and fills Reason from http.StatusText if missing — so
// callers can rely on both being non-empty regardless of the server's
// envelope completeness.
// MaxErrorBodyBytes caps how much of a non-2xx body decodeError will
// hold in memory. 1 MiB is far more than any sane Auth0 error envelope
// (real ones are <1 KiB) but small enough that a misbehaving mock
// can't OOM the test process by returning a multi-GB body.
const maxErrorBodyBytes = 1 << 20

func decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err == nil &&
		(apiErr.StatusCode != 0 || apiErr.ErrorCode != "" || apiErr.Message != "" || apiErr.Reason != "") {
		if apiErr.StatusCode == 0 {
			apiErr.StatusCode = resp.StatusCode
		}
		if apiErr.Reason == "" {
			apiErr.Reason = http.StatusText(resp.StatusCode)
		}
		return &apiErr
	}
	return &APIError{
		StatusCode: resp.StatusCode,
		Reason:     http.StatusText(resp.StatusCode),
		Message:    string(body),
	}
}
