package webserver

// The welcome page: the owner's own captive portal. rbd's firewall sends
// the web requests of devices that haven't accepted it yet to this server
// (render.WelcomePort). It is a separate server on purpose: it only shows
// the page and takes "I agree", and none of the settings API is reachable
// through it.

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"rasbeacon/internal/api"
)

// welcomePolicy is the built-in page's Content-Security-Policy: its own
// inline style, a data: logo and its one form, nothing else.
const welcomePolicy = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; form-action 'self'; " +
	"base-uri 'none'; frame-ancestors 'none'"

// customPolicy is the owner's own page's: sandboxed (no scripts, its own
// origin unknown to the browser, so it can't reach anything of RasBeacon's),
// with forms allowed so it can send "I agree" to /accept.
const customPolicy = "sandbox allow-forms; default-src 'none'; style-src 'unsafe-inline'; img-src data:; " +
	"base-uri 'none'; frame-ancestors 'none'"

// welcomeView is what the built-in page shows.
type welcomeView struct {
	Page api.WelcomePage
	// Logo and Color are the owner's, checked by rbd (see
	// config.WelcomeConfig.Validate), so the template may use them as they
	// are.
	Logo  template.URL
	Color template.CSS
	// Error is why the last "I agree" didn't work; Done shows the
	// "You're connected" page.
	Error string
	Done  bool
}

var welcomeTemplate = template.Must(template.New("welcome").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Page.Title}}</title>
<style>
  :root { color-scheme: light dark; --accent: {{.Color}}; }
  body { margin: 0; font: 17px/1.45 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: Canvas; color: CanvasText; }
  main { max-width: 32rem; margin: 0 auto; padding: 2rem 1rem 3rem; }
  img { display: block; max-width: 10rem; max-height: 6rem; margin: 0 auto 1.5rem; }
  h1 { font-size: 1.75rem; line-height: 1.2; margin: 0 0 .75rem; overflow-wrap: break-word; }
  p { margin: 0 0 1rem; white-space: pre-line; overflow-wrap: break-word; }
  .terms { max-height: 16rem; overflow: auto; padding: .75rem 1rem; border-radius: .75rem;
    background: color-mix(in srgb, CanvasText 6%, Canvas); white-space: pre-line; overflow-wrap: break-word;
    font-size: .94rem; margin-bottom: 1rem; }
  label { display: flex; gap: .6rem; align-items: flex-start; margin-bottom: 1rem; }
  input[type=checkbox] { width: 1.3rem; height: 1.3rem; margin: .1rem 0 0; accent-color: var(--accent); flex: none; }
  input[type=text] { width: 100%; box-sizing: border-box; font: inherit; padding: .7rem .8rem; border-radius: .6rem;
    border: 1px solid color-mix(in srgb, CanvasText 25%, Canvas); background: Canvas; color: CanvasText; }
  .field { display: block; margin-bottom: 1rem; }
  .field span { display: block; font-size: .94rem; margin-bottom: .3rem; }
  button { width: 100%; font: inherit; font-weight: 600; padding: .85rem; border: 0; border-radius: .8rem;
    background: var(--accent); color: #fff; }
  .error { color: #d70015; font-weight: 600; }
</style>
</head>
<body>
<main>
{{if .Logo}}<img src="{{.Logo}}" alt="">{{end}}
{{if .Done}}
<h1>You’re connected</h1>
<p>You can use the internet now. Close this page, or open any website.</p>
{{else}}
<h1>{{.Page.Title}}</h1>
{{if .Page.Message}}<p>{{.Page.Message}}</p>{{end}}
<form method="post" action="/accept">
{{if .Page.Terms}}<div class="terms" tabindex="0" aria-label="Terms">{{.Page.Terms}}</div>
<label><input type="checkbox" name="agree" required> <span>I agree to these terms</span></label>{{end}}
{{if .Page.NeedCode}}<label class="field"><span>Access code</span>
<input type="text" name="code" required autocomplete="off" autocapitalize="none" spellcheck="false"></label>{{end}}
{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
<button type="submit">{{if .Page.Terms}}Accept and continue{{else}}Continue{{end}}</button>
</form>
{{end}}
</main>
</body>
</html>
`))

// WelcomeHandler serves the welcome page on its own port: GET shows it
// (for any address, so phones open it by themselves), POST /accept is
// "I agree".
func (s *Server) WelcomeHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /accept", s.handleWelcomeAccept)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		page, ok := s.welcomePage(w, r)
		if !ok {
			return
		}
		s.showWelcome(w, page, "", !page.Active || page.Accepted)
	})
	return mux
}

// welcomePage asks rbd what to show the device that sent r.
func (s *Server) welcomePage(w http.ResponseWriter, r *http.Request) (api.WelcomePage, bool) {
	var page api.WelcomePage
	status, body, err := s.rbd.Do(http.MethodGet, "/welcome/page?ip="+url.QueryEscape(clientIP(r)), nil)
	if err != nil || status != http.StatusOK || json.Unmarshal(body, &page) != nil {
		http.Error(w, "RasBeacon is starting. Try again in a moment.", http.StatusServiceUnavailable)
		return page, false
	}
	return page, true
}

// showWelcome writes the page: the owner's own, or the built-in one with
// an error or the "You're connected" message.
func (s *Server) showWelcome(w http.ResponseWriter, page api.WelcomePage, problem string, done bool) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Content-Type", "text/html; charset=utf-8")
	if page.CustomPage != "" && !done && problem == "" {
		h.Set("Content-Security-Policy", customPolicy)
		_, _ = w.Write([]byte(page.CustomPage))
		return
	}
	h.Set("Content-Security-Policy", welcomePolicy)
	if page.Title == "" {
		page.Title = "Welcome"
	}
	view := welcomeView{Page: page, Error: problem, Done: done, Color: "#0c8a9c"}
	if page.Color != "" {
		view.Color = template.CSS(page.Color) //nolint:gosec // checked by rbd: "#" and six hex digits
	}
	if page.Logo != "" {
		view.Logo = template.URL(page.Logo) //nolint:gosec // checked by rbd: a PNG or JPEG data: URL
	}
	_ = welcomeTemplate.Execute(w, view)
}

// handleWelcomeAccept is "I agree": rbd checks the code (if any) and lets
// the device through.
func (s *Server) handleWelcomeAccept(w http.ResponseWriter, r *http.Request) {
	// Only from the page itself: a form on another website can't accept
	// for a device.
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}
	page, ok := s.welcomePage(w, r)
	if !ok {
		return
	}
	if !page.Active || page.Accepted {
		s.showWelcome(w, page, "", true)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	// The owner's own page decides how it asks; the built-in one has a box.
	if page.Terms != "" && page.CustomPage == "" && r.PostFormValue("agree") == "" {
		s.showWelcome(w, page, "Tick the box to accept the terms.", false)
		return
	}
	req, _ := json.Marshal(api.WelcomeAcceptRequest{IP: clientIP(r), Code: strings.TrimSpace(r.PostFormValue("code"))})
	status, body, err := s.rbd.Do(http.MethodPost, "/welcome/accept", req)
	// rbd answers 204 No Content when it let the device through.
	if err != nil || status/100 != 2 {
		var problem api.ErrorResponse
		if err != nil || json.Unmarshal(body, &problem) != nil || problem.Error == "" {
			problem.Error = "Something went wrong. Try again."
		}
		s.showWelcome(w, page, problem.Error, false)
		return
	}
	s.showWelcome(w, page, "", true)
}
