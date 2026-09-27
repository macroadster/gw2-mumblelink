//go:build windows

package mumblelink

import (
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const (
	fileMapRead      = 0x0004
	fileMapAllAccess = 0x000F001F
	pageReadWrite    = 0x04
)

// ReadShared opens the Win32 file mapping GW2 writes (default name
// "MumbleLink"). The game attaches to an existing mapping, so this creates
// one when OpenFileMapping fails, then waits for a real frame.
func ReadShared(name string, wait time.Duration) ([]byte, error) {
	if name == "" {
		name = "MumbleLink"
	}
	k := syscall.NewLazyDLL("kernel32.dll")
	openMapping := k.NewProc("OpenFileMappingA")
	createMapping := k.NewProc("CreateFileMappingA")
	mapView := k.NewProc("MapViewOfFile")
	unmapView := k.NewProc("UnmapViewOfFile")
	closeHandle := k.NewProc("CloseHandle")

	namePtr, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	access := uintptr(fileMapRead)
	h, _, e := openMapping.Call(access, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		access = fileMapAllAccess
		h, _, e = openMapping.Call(access, 0, uintptr(unsafe.Pointer(namePtr)))
	}
	created := false
	if h == 0 {
		// INVALID_HANDLE_VALUE, pagefile-backed, read/write so GW2 can open it.
		h, _, e = createMapping.Call(
			^uintptr(0),
			0,
			pageReadWrite,
			0,
			uintptr(LinkSize),
			uintptr(unsafe.Pointer(namePtr)),
		)
		created = true
		access = fileMapAllAccess
	}
	if h == 0 {
		if e == nil || e == syscall.Errno(0) {
			return nil, fmt.Errorf("mumblelink: open %q failed", name)
		}
		return nil, fmt.Errorf("mumblelink: open %q: %w", name, e)
	}
	defer closeHandle.Call(h)

	view, _, e := mapView.Call(h, access, 0, 0, uintptr(LinkSize))
	if view == 0 && access != fileMapRead {
		view, _, e = mapView.Call(h, fileMapRead, 0, 0, uintptr(LinkSize))
	}
	if view == 0 {
		if e == nil || e == syscall.Errno(0) {
			return nil, fmt.Errorf("mumblelink: map %q failed", name)
		}
		return nil, fmt.Errorf("mumblelink: map %q: %w", name, e)
	}
	defer unmapView.Call(view)

	deadline := time.Now().Add(wait)
	for {
		out := make([]byte, LinkSize)
		src := unsafe.Slice((*byte)(unsafe.Pointer(view)), LinkSize)
		copy(out, src)
		if FrameReady(out) {
			return out, nil
		}
		if time.Now().After(deadline) {
			how := "opened"
			if created {
				how = "created"
			}
			return nil, fmt.Errorf("mumblelink: %s %q but Guild Wars 2 has not written a frame. Be in the world on a character, without -mumble 0. If the game was already running, restart it and run this again so it can attach", how, name)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// WriteMappingFile reads one frame and writes the raw 5460 bytes to path.
// The Windows helper uses this so the macOS side can open a normal file.
func WriteMappingFile(path, name string, wait time.Duration) error {
	b, err := ReadShared(name, wait)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
