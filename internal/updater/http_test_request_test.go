package updater

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

// newTestRequest is httptest.NewRequest with the loopback Host a browser
// sends to PatchBoard when the target is a bare path. httptest's default
// Host, example.com, is rejected by trustedHost.
func newTestRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	if strings.HasPrefix(target, "/") {
		request.Host = fmt.Sprintf("127.0.0.1:%d", defaultPort)
	}
	return request
}
