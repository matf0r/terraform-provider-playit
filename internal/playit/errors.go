package playit

import (
	"errors"
	"fmt"
)

// Error kinds reported by the API under {"status":"error"}.
const (
	KindValidation   = "validation"
	KindPathNotFound = "path-not-found"
	KindAuth         = "auth"
	KindInternal     = "internal"
)

// FailError is a business-level failure.
//
// The playit API reports these with HTTP 200 and an envelope of
// {"status":"fail","data":"<Code>"}. Code is the server-side enum variant name.
// Unlike the value enums elsewhere in this API, the error enums carry no serde
// rename attributes, so they arrive verbatim in PascalCase: "TunnelNotFound",
// "ChangingAgentIdNotAllowed", and so on.
//
// A FailError is an outcome, not a fault: it is never retried.
type FailError struct {
	Path string
	Code string
}

func (e *FailError) Error() string {
	return fmt.Sprintf("playit: %s: %s", e.Path, e.Code)
}

// APIError is a protocol-level failure, reported as
// {"status":"error","data":{"type":"<kind>","message":<string|object>}}.
//
// Note the content key is "message", not "data" as in every other union in this
// API, and that its shape varies by kind: a bare string for auth and validation,
// an object for path-not-found and internal.
type APIError struct {
	Path    string
	Kind    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("playit: %s: api error (%s)", e.Path, e.Kind)
	}
	return fmt.Sprintf("playit: %s: api error (%s): %s", e.Path, e.Kind, e.Message)
}

// TransportError covers everything below the envelope: connection failures,
// unparseable bodies, rate limiting and 5xx responses.
type TransportError struct {
	Path       string
	StatusCode int
	Err        error
}

func (e *TransportError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("playit: %s: http %d: %v", e.Path, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("playit: %s: %v", e.Path, e.Err)
}

func (e *TransportError) Unwrap() error { return e.Err }

// IsFail reports whether err is a FailError carrying the given code.
//
// This is the single predicate the resource layer needs: IsFail(err,
// "TunnelNotFound") drives both drift detection on read and idempotent destroy.
func IsFail(err error, code string) bool {
	var fe *FailError
	return errors.As(err, &fe) && fe.Code == code
}

// IsAuth reports whether err is an authentication or authorization failure,
// used to fail fast during provider configuration.
func IsAuth(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Kind == KindAuth
}
