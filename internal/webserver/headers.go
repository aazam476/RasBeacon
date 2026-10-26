package webserver

// This file adds the security headers to every answer: the Content-Security-
// Policy, no framing, and no guessing of file types.

import "net/http"

// contentSecurityPolicy tells the browser what RasBeacon's pages may load
// and run. Everything comes from rbweb itself ('self'): no inline scripts,
// no scripts from other sites and no "javascript:" addresses, so a bug that
// lets someone else's text into the page can't run it as code. Pictures may
// also be "data:" addresses (the login browser's screenshots, plugin
// icons). frame-ancestors 'none' stops other websites from showing
// RasBeacon's pages inside their own ("clickjacking").
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// withSecurityHeaders adds the browser protections to every answer.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		// The same as frame-ancestors, for browsers that only know this one.
		h.Set("X-Frame-Options", "DENY")
		// Files are only what their Content-Type says, never guessed:
		// a plugin log served as text can't turn into a page.
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
