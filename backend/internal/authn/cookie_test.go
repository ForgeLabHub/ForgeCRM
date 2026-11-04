package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/geff1778/forgecrm/internal/config"
)

func TestSessionCookieAttributesAndClearing(t *testing.T) {
	s := NewService(nil, &config.Config{SessionCookieName: "session", CookieSecure: true, CookieSameSite: "none", CookiePartitioned: true}, nil, nil, nil)
	w := httptest.NewRecorder()
	s.SetSessionCookie(w, "token", time.Now().Add(time.Hour))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies: %v", cookies)
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || !c.Partitioned || c.SameSite != http.SameSiteNoneMode || c.Path != "/" || c.MaxAge <= 0 {
		t.Fatalf("unexpected cookie: %+v", c)
	}
	w = httptest.NewRecorder()
	s.ClearSessionCookie(w)
	c = w.Result().Cookies()[0]
	if c.MaxAge != -1 || !c.Partitioned || c.SameSite != http.SameSiteNoneMode || !c.Secure || c.Path != "/" {
		t.Fatalf("unexpected cleared cookie: %+v", c)
	}
}
