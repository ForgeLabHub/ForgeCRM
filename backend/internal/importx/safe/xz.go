package safe

import (
	"fmt"
	"io"

	"github.com/ulikunitz/xz"
)

// newXZReader wraps a reader with XZ decompression. XZ is not in the Go standard
// library, so a pure-Go implementation is used rather than shelling out to the
// xz binary: spawning a process on untrusted input would widen the attack
// surface for no benefit.
func newXZReader(r io.Reader) (io.Reader, error) {
	xr, err := xz.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("open xz: %w", err)
	}
	return xr, nil
}
