package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jachavarro2004/jhon-agent/engine/internal/release"
	"github.com/jachavarro2004/jhon-agent/engine/internal/version"
)

// The most a download may hold: the API's answer, a .sha256 line, a
// signature bundle, a tarball (a release's is tens of MB). A larger body is
// refused before it is used.
var (
	maxAPI     int64 = 1 << 20
	maxSum     int64 = 4 << 10
	maxBundle  int64 = 1 << 20
	maxTarball int64 = 200 << 20
)

// httpsRedirects is the client's redirect policy when it is https only: a
// redirect may go to https alone.
func httpsRedirects(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("the redirect to %s is not https", req.URL)
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

// statusError is an answer other than 200 OK.
type statusError struct {
	url, status string
	code        int
}

func (e *statusError) Error() string { return e.url + " answered " + e.status }

// get writes rawURL's body to w, or says what failed: the scheme, the
// network, the status, or a body larger than limit, which names it name.
func (c *Client) get(ctx context.Context, rawURL, name string, w io.Writer, limit int64) error {
	if u, err := url.Parse(rawURL); c.HTTPSOnly && (err != nil || u.Scheme != "https") {
		return fmt.Errorf("%s is not an https URL", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %v", rawURL, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json, application/octet-stream")
	req.Header.Set("User-Agent", "jhon/"+version.Version)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue interface{ Unwrap() error }
		if errors.As(err, &ue) {
			err = ue.Unwrap()
		}
		return fmt.Errorf("cannot reach %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return &statusError{rawURL, resp.Status, resp.StatusCode}
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("cannot read %s: %v", rawURL, err)
	}
	if n > limit {
		return fmt.Errorf("%s is larger than %d bytes", name, limit)
	}
	return nil
}

// download writes a's body to dir/<a.Name>, at most limit bytes.
func (c *Client) download(ctx context.Context, a Asset, dir string, limit int64) error {
	f, err := os.Create(filepath.Join(dir, a.Name))
	if err != nil {
		return err
	}
	err = c.get(ctx, a.URL, a.Name, f, limit)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// stamped is the version build/jhon in the tarball artifact reports, as
// release.InstallArtifact reads it: the binary alone is unpacked into a
// directory of dir, and run with `version --json`.
func stamped(artifact, dir string) (string, error) {
	out, err := os.MkdirTemp(dir, "stamp-")
	if err != nil {
		return "", err
	}
	name := filepath.Base(artifact)
	if b, err := exec.Command("tar", "-xzf", artifact, "-C", out, release.Binary).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%s holds no %s: %s", name, release.Binary, release.Substituted(b))
	}
	b, err := exec.Command(filepath.Join(out, release.Binary), "version", "--json").Output()
	var v struct{ Version string }
	if err != nil || json.Unmarshal(b, &v) != nil || v.Version == "" {
		return "", fmt.Errorf("%s's %s reports no version", name, release.Binary)
	}
	return v.Version, nil
}
