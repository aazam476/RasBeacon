package update

import (
	"archive/tar"
	"fmt"
	"io"
)

// Target is a partition to write: the update bundle's entry for it is
// "boot" or "system".
type Target struct {
	Device io.WriterAt
	Size   int64
}

// WriteBundle reads the uncompressed bundle (a tar of sparse images) and
// writes each image to its target. Every target must be written exactly
// once.
func WriteBundle(bundle io.Reader, targets map[string]Target) error {
	t := tar.NewReader(bundle)
	written := map[string]bool{}
	for {
		header, err := t.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading the bundle: %w", err)
		}
		target, ok := targets[header.Name]
		if !ok || written[header.Name] {
			return fmt.Errorf("unexpected %q in the bundle", header.Name)
		}
		if _, err := WriteSparse(target.Device, t, target.Size); err != nil {
			return fmt.Errorf("%s partition: %w", header.Name, err)
		}
		written[header.Name] = true
	}
	for name := range targets {
		if !written[name] {
			return fmt.Errorf("the bundle has no %s partition", name)
		}
	}
	return nil
}
