package auth0mock

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// do is the shared HTTP plumbing every sub-client routes through.
//
//   - method, path: HTTP method and the full path under baseURL
//     (e.g. "/admin0/expectations"). Always starts with "/".
//   - body: optional request body; non-nil values are JSON-marshalled
//     and sent with Content-Type: application/json. Pass nil for no
//     body (NewRequestWithContext gets http.NoBody, which is the right
//     marker for "deliberately empty" so chunked encoding stays off).
//   - out:  optional response-decode target; non-nil values are
//     JSON-decoded from the response. Pass nil to discard the body.
//
// Returns *APIError on a non-2xx response (decoded from the Auth0
// envelope when present, synthesized from the status line otherwise),
// or a wrapped transport error.
func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("auth0mock: marshal %s %s body: %w", method, path, err)
		}
		rdr = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("auth0mock: build %s %s request: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("auth0mock: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return decodeError(resp)
	}
	if out != nil {
		// Tolerate empty bodies — older mock builds (and most stub
		// test servers) reply 204 with no body even on POSTs that
		// the newer server answers with {"id": ...}. Leaving *out at
		// its zero value is the closest equivalent we can offer.
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("auth0mock: decode %s %s response: %w", method, path, err)
		}
	}
	return nil
}
