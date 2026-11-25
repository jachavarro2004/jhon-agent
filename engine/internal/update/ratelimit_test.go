package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// T8c (odd/tasks/studio-integration-v1.md L45): GitHub's API answers 403 or
// 429 when the rate limit of the address is used up (X-RateLimit-Remaining:
// 0, or the documented body); jhon says so, with the reset time when GitHub
// gives one, and, as it sends no token, to retry after the reset. Any other
// status, a 403 that is not the rate limit among them, still says what was
// answered.
func TestARateLimitAnswerIsNamedWithItsReset(t *testing.T) {
	const (
		primary   = `{"message":"API rate limit exceeded for 203.0.113.7. (But here's the good news: Authenticated requests get a higher rate limit. Check out the documentation for more details.)","documentation_url":"https://docs.github.com/rest/overview/resources-in-the-rest-api#rate-limiting"}`
		secondary = `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again.","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`
		advice    = "; jhon sends no GitHub token, so retry after the reset"
	)
	for _, c := range []struct {
		name    string
		code    int
		headers map[string]string
		body    string
		want    string
	}{
		{"a 403 with no requests left names the reset", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1795000000"}, primary,
			" refused: GitHub's API rate limit for this address is used up (403 Forbidden, X-RateLimit-Remaining: 0); it resets at 2026-11-18 11:06:40 UTC" + advice},
		{"a 429 with no requests left names the reset", 429, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1795000000"}, "",
			" refused: GitHub's API rate limit for this address is used up (429 Too Many Requests, X-RateLimit-Remaining: 0); it resets at 2026-11-18 11:06:40 UTC" + advice},
		{"the documented body alone is the rate limit", 403, nil, primary,
			" refused: GitHub's API rate limit for this address is used up (403 Forbidden); jhon sends no GitHub token, so retry later, once it resets"},
		{"a secondary limit's Retry-After is its reset", 403, map[string]string{"Retry-After": "60"}, secondary,
			" refused: GitHub's API rate limit for this address is used up (403 Forbidden); it resets in 60 s" + advice},
		{"a 429 is the rate limit", 429, nil, "",
			" refused: GitHub's API rate limit for this address is used up (429 Too Many Requests); jhon sends no GitHub token, so retry later, once it resets"},
		{"an unreadable reset is not given", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "soon"}, "",
			" refused: GitHub's API rate limit for this address is used up (403 Forbidden, X-RateLimit-Remaining: 0); jhon sends no GitHub token, so retry later, once it resets"},
		// PRESERVE: a 403 that is not the rate limit, and other statuses.
		{"a 403 with requests left is what it answered", 403, map[string]string{"X-RateLimit-Remaining": "59"}, `{"message":"Resource not accessible by integration"}`,
			" answered 403 Forbidden"},
		{"a 404 is what it answered", 404, map[string]string{"X-RateLimit-Remaining": "0"}, primary, " answered 404 Not Found"},
		{"a 500 is what it answered", 500, nil, "", " answered 500 Internal Server Error"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range c.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(c.code)
				w.Write([]byte(c.body))
			}))
			defer srv.Close()
			u := srv.URL + "/repos/jachavarro2004/jhon-studio/releases?per_page=30"
			cl := &Client{URL: srv.URL, HTTP: srv.Client()}
			_, err := cl.ToolReleases(context.Background(), Tool{Binary: "jhon-studio", Owner: "jachavarro2004", Repo: "jhon-studio"}, false)
			if err == nil || err.Error() != u+c.want {
				t.Fatalf("error = %v\nwant    %s", err, u+c.want)
			}
		})
	}
}

// T8c: jhon's own latest release read under the rate limit says so too,
// and a rate-limited /releases/latest is no "no stable release".
func TestLatestUnderTheRateLimitSaysSo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1795000000")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	u := srv.URL + "/repos/jachavarro2004/jhon-agent/releases/latest"
	_, err := (&Client{URL: u, HTTP: srv.Client()}).Latest(context.Background())
	want := u + " refused: GitHub's API rate limit for this address is used up (403 Forbidden, X-RateLimit-Remaining: 0); it resets at 2026-11-18 11:06:40 UTC; jhon sends no GitHub token, so retry after the reset"
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v\nwant    %s", err, want)
	}
}
