package server

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOAuthPageLoadsSameOriginBrandWithoutRelaxingPagePolicy(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/oauth/connections", nil)
	(&mcpGateway{}).renderOAuthPage(c, oauthPage{Title: "已授权的 AI 应用", Email: "creator@example.test"})
	if w.Code != 200 {
		t.Fatalf("authorization page status = %d", w.Code)
	}
	for _, directive := range []string{"default-src 'none'", "img-src 'self'", "form-action 'self'", "frame-ancestors 'none'", "base-uri 'none'"} {
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), directive) {
			t.Fatalf("missing authorization page policy %q", directive)
		}
	}
	if !strings.Contains(w.Body.String(), `src="/favicon.svg?v=bubu-2"`) || !strings.Contains(w.Body.String(), ">Bubu</a>") || strings.Contains(w.Body.String(), "帧间") {
		t.Fatal("authorization page did not render the Bubu brand")
	}
}
