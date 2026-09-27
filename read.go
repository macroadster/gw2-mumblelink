package mumblelink

import (
	"fmt"
	"os"
)

// ReadFile loads one snapshot from a copied MumbleLink block.
// The file must be at least LinkSize bytes. This is the PoC door on a
// machine that is not the game host: copy /dev/shm/MumbleLink (Wine) or
// a Windows named-mapping dump, then point the tool at that file.
func ReadFile(path string) (Snapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, err
	}
	if len(b) < LinkSize {
		return Snapshot{}, fmt.Errorf("mumblelink: %s is %d bytes, need %d", path, len(b), LinkSize)
	}
	return Decode(b)
}
