//go:build darwin

package mumblelink

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type wineHost struct {
	Bin    string
	Prefix string
	Extra  map[string]string
}

func (h wineHost) env() []string {
	drop := map[string]bool{
		"WINEPREFIX": true, "WINELOADER": true, "WINEDEBUG": true,
		"WINELOADERNOEXEC": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] || h.Extra[k] != "" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"WINEPREFIX="+h.Prefix,
		"WINELOADER="+h.Bin,
		"WINEDEBUG=-all",
	)
	for k, v := range h.Extra {
		if v == "" || drop[k] {
			continue
		}
		env = append(env, k+"="+v)
	}
	return env
}

// findWineHost locates the wineserver that is running Gw2-64.exe and the
// prefix it was started with. WINE and WINEPREFIX override discovery.
func findWineHost() (wineHost, error) {
	overrideBin, overridePrefix := os.Getenv("WINE"), os.Getenv("WINEPREFIX")
	out, err := exec.Command("ps", "-axww", "-o", "pid=", "-o", "command=").Output()
	if err != nil {
		return wineHost{}, fmt.Errorf("mumblelink: ps: %w", err)
	}
	var servers []int
	gw2 := false
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pidStr, cmd, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		if strings.Contains(cmd, "Gw2-64.exe") {
			gw2 = true
		}
		if strings.Contains(cmd, "wineserver") && !strings.Contains(cmd, "grep") {
			servers = append(servers, pid)
		}
	}
	if !gw2 {
		return wineHost{}, fmt.Errorf("mumblelink: Guild Wars 2 is not running (no Gw2-64.exe). Start the game, enter the world, then retry")
	}
	var last error
	for _, pid := range servers {
		h, err := hostFromServer(pid)
		if err != nil {
			last = err
			continue
		}
		if _, err := os.Stat(h.Bin); err != nil {
			last = err
			continue
		}
		if st, err := os.Stat(h.Prefix); err != nil || !st.IsDir() {
			last = fmt.Errorf("prefix %s: %v", h.Prefix, err)
			continue
		}
		if overrideBin != "" {
			h.Bin = overrideBin
		}
		if overridePrefix != "" {
			h.Prefix = overridePrefix
		}
		return h, nil
	}
	if last == nil {
		last = fmt.Errorf("no wineserver process")
	}
	return wineHost{}, fmt.Errorf("mumblelink: Gw2-64.exe is running but its Wine prefix could not be found: %w", last)
}

func hostFromServer(pid int) (wineHost, error) {
	_, env, err := procArgs(pid)
	if err != nil {
		return wineHost{}, err
	}
	vals := map[string]string{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if ok {
			vals[k] = v
		}
	}
	bin := vals["WINELOADER"]
	prefix := vals["WINEPREFIX"]
	if bin == "" || prefix == "" {
		return wineHost{}, fmt.Errorf("wineserver %d has no WINELOADER/WINEPREFIX", pid)
	}
	bin = filepath.Clean(bin)
	h := wineHost{Bin: bin, Prefix: prefix, Extra: map[string]string{}}
	for k, v := range vals {
		if wineEnvKey(k) {
			h.Extra[k] = v
		}
	}
	delete(h.Extra, "WINELOADER")
	delete(h.Extra, "WINEPREFIX")
	delete(h.Extra, "WINEDEBUG")
	delete(h.Extra, "WINELOADERNOEXEC")
	fillDerived(&h)
	return h, nil
}

// wineEnvKey reports keys a new wine process needs in order to join the
// wineserver that is already running the game. WINEMSYNC in particular must
// match, or the client dies with SIGFPE before main.
func wineEnvKey(k string) bool {
	switch {
	case strings.HasPrefix(k, "WINE"), strings.HasPrefix(k, "CX_"), strings.HasPrefix(k, "DYLD"):
		return true
	case strings.HasPrefix(k, "MVK_"), strings.HasPrefix(k, "MTL_"), strings.HasPrefix(k, "GST_"):
		return true
	case strings.HasPrefix(k, "ROSETTA"), strings.HasPrefix(k, "DOTNET"):
		return true
	}
	return false
}

func fillDerived(h *wineHost) {
	if h.Extra == nil {
		h.Extra = map[string]string{}
	}
	wineDir := filepath.Dir(filepath.Dir(h.Bin)) // .../wine/bin/wine -> .../wine
	if h.Extra["WINEDLLPATH"] == "" {
		h.Extra["WINEDLLPATH"] = strings.Join([]string{
			filepath.Join(wineDir, "lib", "x86_64-windows"),
			filepath.Join(wineDir, "lib", "i386-windows"),
			filepath.Join(wineDir, "lib", "wine"),
		}, ":")
	}
	if h.Extra["DYLD_FALLBACK_LIBRARY_PATH"] == "" {
		app := filepath.Dir(filepath.Dir(filepath.Dir(wineDir))) // Contents/SharedSupport/wine -> .app
		// wineDir is SharedSupport/wine, parent SharedSupport, parent Contents, parent .app
		contents := filepath.Dir(filepath.Dir(wineDir))
		h.Extra["DYLD_FALLBACK_LIBRARY_PATH"] = strings.Join([]string{
			filepath.Join(contents, "Frameworks", "moltenvkcx"),
			filepath.Join(wineDir, "lib"),
			filepath.Join(wineDir, "lib64"),
			filepath.Join(contents, "Frameworks"),
		}, ":")
		_ = app
	}
}

func procArgs(pid int) (args []string, env []string, err error) {
	mib := []int32{1, 49, int32(pid)} // CTL_KERN, KERN_PROCARGS2
	n, err := sysctlSize(mib)
	if err != nil {
		return nil, nil, err
	}
	buf := make([]byte, n)
	buf, err = sysctl(mib, buf)
	if err != nil {
		return nil, nil, err
	}
	args, env = parseProcArgs2(buf)
	return args, env, nil
}

func sysctlSize(mib []int32) (uintptr, error) {
	var n uintptr
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		uintptr(len(mib)),
		0,
		uintptr(unsafe.Pointer(&n)),
		0,
		0,
	)
	if errno != 0 {
		return 0, errno
	}
	return n, nil
}

func sysctl(mib []int32, buf []byte) ([]byte, error) {
	n := uintptr(len(buf))
	_, _, errno := syscall.Syscall6(
		syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])),
		uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)),
		0,
		0,
	)
	if errno != 0 {
		return nil, errno
	}
	if n > uintptr(len(buf)) {
		n = uintptr(len(buf))
	}
	return buf[:n], nil
}

func parseProcArgs2(data []byte) (args []string, env []string) {
	if len(data) < 4 {
		return nil, nil
	}
	argc := int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]
	i := bytes.IndexByte(data, 0)
	if i < 0 {
		return nil, nil
	}
	data = data[i+1:]
	for len(data) > 0 && data[0] == 0 {
		data = data[1:]
	}
	parts := bytes.Split(data, []byte{0})
	for len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	strs := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		strs = append(strs, string(p))
	}
	if argc > len(strs) {
		argc = len(strs)
	}
	if argc < 0 {
		argc = 0
	}
	return strs[:argc], strs[argc:]
}
