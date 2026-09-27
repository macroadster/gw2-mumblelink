//go:build !windows

package mumblelink

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// ReadShared reads one MumbleLink frame from Guild Wars 2 running under Wine
// on this machine. It builds the Windows half of this command, runs it inside
// the game's prefix, and decodes the file that helper writes.
func ReadShared(name string, wait time.Duration) ([]byte, error) {
	if name == "" {
		name = "MumbleLink"
	}
	if wait < 0 {
		wait = 0
	}
	helper, err := ensureHelper()
	if err != nil {
		return nil, err
	}
	host, err := findWineHost()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "gw2ml-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "MumbleLink.bin")
	wineOut := toWinePath(out)

	cmd := exec.Command(host.Bin, helper, "raw", wineOut, name, fmt.Sprintf("%d", wait.Milliseconds()))
	cmd.Env = host.env()
	cmd.Dir = host.Prefix
	stderr, err := cmd.CombinedOutput()
	if err != nil {
		msg := trimOutput(stderr)
		if msg == "" {
			return nil, fmt.Errorf("mumblelink: wine reader: %w", err)
		}
		return nil, fmt.Errorf("mumblelink: wine reader: %w: %s", err, msg)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		return nil, fmt.Errorf("mumblelink: wine reader wrote no frame: %w", err)
	}
	if len(b) < LinkSize {
		return nil, fmt.Errorf("mumblelink: wine reader wrote %d bytes, need %d", len(b), LinkSize)
	}
	return b[:LinkSize], nil
}

// WriteMappingFile is the in-prefix entry point. On macOS the raw subcommand
// is the Windows build, launched by ReadShared.
func WriteMappingFile(path, name string, wait time.Duration) error {
	return fmt.Errorf("mumblelink: raw must run under Wine (refusing to write %s for %s, wait %s)", path, name, wait)
}

func toWinePath(unix string) string {
	p := filepath.Clean(unix)
	if !filepath.IsAbs(p) {
		abs, err := filepath.Abs(p)
		if err == nil {
			p = abs
		}
	}
	out := `Z:`
	for _, part := range splitPath(p) {
		out += `\` + part
	}
	return out
}

func splitPath(p string) []string {
	var parts []string
	for {
		dir, file := filepath.Split(p)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		if dir == "" || dir == p {
			break
		}
		p = filepath.Clean(dir)
	}
	return parts
}

func ensureHelper() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dest := filepath.Join(cache, "gw2-mumblelink", "gw2-mumblelink.exe")
	root, rootErr := moduleRoot()
	if rootErr == nil {
		src := filepath.Join(root, "read_windows.go")
		if newerThan(src, dest) {
			if err := buildHelper(root, dest); err != nil {
				return "", err
			}
		}
	}
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return dest, nil
	}
	if rootErr != nil {
		return "", fmt.Errorf("mumblelink: windows reader is not built and the module source was not found: %w", rootErr)
	}
	return "", fmt.Errorf("mumblelink: windows reader was not built at %s", dest)
}

func newerThan(src, dest string) bool {
	ss, err := os.Stat(src)
	if err != nil {
		return false
	}
	ds, err := os.Stat(dest)
	if err != nil {
		return true
	}
	return ss.ModTime().After(ds.ModTime())
}

func buildHelper(root, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("go", "build", "-o", dest, "./cmd/gw2-mumblelink")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mumblelink: build windows reader: %w: %s", err, trimOutput(out))
	}
	return nil
}

func moduleRoot() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("no source path")
	}
	dir := filepath.Dir(file)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return "", fmt.Errorf("go.mod next to %s: %w", file, err)
	}
	return dir, nil
}

func trimOutput(b []byte) string {
	s := string(b)
	if len(s) > 800 {
		s = s[len(s)-800:]
	}
	return s
}
