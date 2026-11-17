// Package integrations carries the integration manifests,
// integrations/<name>/manifest.yaml (schema jhon.integration/v1), into the
// binary. engine/internal/integrations reads and validates them; nothing else
// imports this package.
package integrations

import "embed"

// FS holds every integrations/<name>/manifest.yaml.
//
//go:embed */manifest.yaml
var FS embed.FS
