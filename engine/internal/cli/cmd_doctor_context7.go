package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jachavarro2004/jhon-agent/engine/internal/render"
	"github.com/jachavarro2004/jhon-agent/engine/internal/settings"
)

// context7 is doctor's row for context7 (T133e, L1016), after the versions:
// it is a hosted endpoint, so there is no version to compare, and whether it
// answers is asked only with --remote, never by a plain doctor or at a
// session's start. Not answering is a warning: its tools then answer with an
// error, and nothing else of Jhon depends on it.
func (d doctor) context7(remote bool) {
	o := d.o
	switch {
	case !settings.On(render.Context7Setting):
		o.Note(fmt.Sprintf("context7 is off (%s = false)", render.Context7Setting))
	case !remote:
		o.Note(fmt.Sprintf("context7 is the hosted %s, for explore and worker: nothing installed or pinned; run 'jhon doctor --remote' to ask whether it answers", render.Context7URL))
	default:
		if err := probeContext7(render.Context7URL); err != nil {
			o.Warn(fmt.Sprintf("context7 unreachable (%s: %s) — offline, or the service is down; its tools answer with an error until it is back", render.Context7URL, err))
			return
		}
		o.OK("context7 answers at " + render.Context7URL)
	}
}

// probeContext7 asks endpoint one MCP initialize, with no credential, bounded to
// 5 seconds: nil when it answers 200.
var probeContext7 = func(endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const initialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"jhon-doctor","version":"0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(initialize))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The row names the endpoint itself: only why it did not answer.
		var sent *url.Error
		if errors.As(err, &sent) {
			return sent.Err
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
