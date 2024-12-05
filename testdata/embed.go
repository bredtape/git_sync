package testdata

import _ "embed"

// full bundle
//
//go:embed full.bundle
var FullBundle []byte

// partial bundle with the last n=1 commit
//
//go:embed last.bundle
var LastBundle []byte
