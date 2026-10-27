package apireq

import (
	"log"
	"net/http"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// TooMany renders the 429 rate-limit response (Retry-After:1, JSON shape
// {message, status:429} matching the ingest/fetch 429 shape). msg stays
// a param (e.g. "Rate limit exceeded." vs "Too many concurrent streams.")
// so callers keep their exact user-visible text.
func TooMany(re *core.RequestEvent, msg string) error {
	re.Response.Header().Set("Retry-After", "1")
	return re.JSON(http.StatusTooManyRequests, map[string]any{
		"message": msg,
		"status":  http.StatusTooManyRequests,
	})
}

// Internal logs err server-side and renders a generic 500: DB/driver
// text never reaches the caller. logMsg carries the caller's prefix
// (e.g. "account: failed to list superusers") so log lines are unchanged.
func Internal(re *core.RequestEvent, logMsg string, err error) error {
	if err != nil {
		log.Printf("%s: %v", logMsg, err)
	} else {
		log.Print(logMsg)
	}
	return re.JSON(http.StatusInternalServerError, map[string]any{
		"message": "internal error",
		"status":  http.StatusInternalServerError,
	})
}
