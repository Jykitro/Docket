// Package installer holds the plandillo-cli install scripts. They download the CLI from a GitHub release, check its
// checksum and sign it in to PLANDILLO_URL; a board serves them at /api/cli/install.sh and /api/cli/install.ps1.
package installer

import _ "embed"

var (
	//go:embed install.sh
	Sh string
	//go:embed install.ps1
	Ps1 string
)
