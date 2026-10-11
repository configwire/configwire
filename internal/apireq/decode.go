// Package apireq holds shared helpers for API request handling.
package apireq

import (
	"bytes"
	"io"

	"github.com/pocketbase/pocketbase/core"
)

// Empty bodies -> 400 (BindBody alone would silently return nil);
// malformed JSON or wrong field types -> 400. Body bytes are the
// emptiness source of truth (ContentLength is -1 for chunked).
func DecodeBody(re *core.RequestEvent, dst any) error {
	if re.Request.ContentLength == 0 {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	if re.Request.Body == nil {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	raw, err := io.ReadAll(io.LimitReader(re.Request.Body, 1<<20+1))
	if err != nil {
		return re.BadRequestError("malformed JSON body: "+err.Error(), nil)
	}
	// Restore for BindBody.
	re.Request.Body = io.NopCloser(bytes.NewReader(raw))
	if len(bytes.TrimSpace(raw)) == 0 {
		return re.BadRequestError("empty body: expected a JSON object.", nil)
	}
	if err := re.BindBody(dst); err != nil {
		return re.BadRequestError("malformed JSON body: "+err.Error(), nil)
	}
	return nil
}
