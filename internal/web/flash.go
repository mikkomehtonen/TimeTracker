package web

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	flashCookie = "tt_flash"
	flashMaxAge = 2 * time.Minute
)

// setFlash stores a one-shot message carried through the PRG redirect.
func setFlash(w http.ResponseWriter, r *http.Request, kind, msg string) {
	setCookie(w, r, flashCookie, url.QueryEscape(kind+"|"+msg), flashMaxAge)
}

// takeFlash reads the pending flash message; render clears it immediately so
// it is shown exactly once.
func takeFlash(r *http.Request) (kind, msg string, ok bool) {
	raw := cookieValue(r, flashCookie)
	if raw == "" {
		return "", "", false
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		return "", "", false
	}
	k, m, found := strings.Cut(decoded, "|")
	if !found || (k != "ok" && k != "err") {
		return "", "", false
	}
	return k, m, true
}
