package utils

import (
	"fmt"
	"net/http"
	"strings"
)

// HandleAPIError checks the HTTP response status and returns an error if not 2xx.
// It reads and logs the response body for debugging purposes.
func HandleAPIError(resp *http.Response) error {
	if err := CheckStatus(resp); err != nil {
		bodyBytes, readErr := ReadResponseBody(resp)
		resp.Body.Close()
		if readErr == nil && len(bodyBytes) > 0 {
			Debugf("API error body: %s", strings.TrimSpace(string(bodyBytes)))
		}
		return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}
	return nil
}
