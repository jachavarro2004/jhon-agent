package setupui

import "errors"

// Release is what the update screens show: the version installed, the
// latest release, and the one a roll-back returns to.
type Release struct {
	Current, Latest string
	// Previous is the version kept for a roll-back; empty when none is kept.
	Previous string
	// Date is the latest release's date; Source the repository it comes from.
	Date, Source string
	// Verified is whether the latest release's signature checked out, and
	// Signature how it was signed.
	Verified  bool
	Signature string
	Notes     []string
	// NoStable is whether no stable release is published yet (only
	// pre-releases, S11): Latest is Current and nothing is offered.
	NoStable bool
}

// Updater finds, applies and rolls back releases: `jhon update`'s backend
// (T6, the CLI's on internal/update). Latest's error is shown as the reason
// the check failed.
type Updater interface {
	Latest() (Release, error)
	Update(Release) error
	// Rollback reinstalls version, the one kept before the last update.
	Rollback(version string) error
}

// ErrUpdateNotAvailable is UpdatesNotAvailableYet's answer to everything.
var ErrUpdateNotAvailable = errors.New("updates are not available in this build yet")

// UpdatesNotAvailableYet is the Updater of a System given none (the tests'):
// it finds no release and changes nothing, and the update screen says so
// instead of blaming the network. The CLI wires the real one.
type UpdatesNotAvailableYet struct{}

func (UpdatesNotAvailableYet) Latest() (Release, error) { return Release{}, ErrUpdateNotAvailable }
func (UpdatesNotAvailableYet) Update(Release) error     { return ErrUpdateNotAvailable }
func (UpdatesNotAvailableYet) Rollback(string) error    { return ErrUpdateNotAvailable }
