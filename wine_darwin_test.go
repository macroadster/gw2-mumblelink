//go:build darwin

package mumblelink

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestParseProcArgs2(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{2, 0, 0, 0})
	b.WriteString("/bin/wineserver")
	b.WriteByte(0)
	b.WriteByte(0)
	b.WriteString("/bin/wineserver")
	b.WriteByte(0)
	b.WriteString("arg")
	b.WriteByte(0)
	b.WriteString("WINEPREFIX=/tmp/prefix")
	b.WriteByte(0)
	b.WriteString("WINELOADER=/tmp/wine")
	b.WriteByte(0)
	b.WriteByte(0)
	args, env := parseProcArgs2(b.Bytes())
	if len(args) != 2 || args[1] != "arg" {
		t.Fatalf("args: %#v", args)
	}
	if len(env) != 2 || env[0] != "WINEPREFIX=/tmp/prefix" {
		t.Fatalf("env: %#v", env)
	}
}

func TestProcArgsLiveShape(t *testing.T) {
	// argc is little-endian; guard the fixture against a bad width.
	raw := make([]byte, 4)
	binary.LittleEndian.PutUint32(raw, 1)
	if raw[0] != 1 {
		t.Fatal(raw)
	}
}

func TestToWinePath(t *testing.T) {
	got := toWinePath("/tmp/gw2ml/MumbleLink.bin")
	want := `Z:\tmp\gw2ml\MumbleLink.bin`
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
