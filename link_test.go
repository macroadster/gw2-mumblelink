package mumblelink

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
	"unicode/utf16"
)

func putU16(b []byte, off int, s string) {
	u := utf16.Encode([]rune(s))
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[off+i*2:], v)
	}
}

func putF32(b []byte, off int, v float32) {
	binary.LittleEndian.PutUint32(b[off:], math.Float32bits(v))
}

func sampleFrame() []byte {
	b := make([]byte, LinkSize)
	binary.LittleEndian.PutUint32(b[offVersion:], 2)
	binary.LittleEndian.PutUint32(b[offTick:], 9001)
	putF32(b, offAvatarPos, 10)
	putF32(b, offAvatarPos+4, 20)
	putF32(b, offAvatarPos+8, 30)
	putF32(b, offAvatarPos+12, float32(math.NaN())) // front.x — whole front vec drops
	// front is offAvatarFront, not +12 from pos. Set front to NaN explicitly.
	putF32(b, offAvatarFront, float32(math.NaN()))
	putU16(b, offName, "Guild Wars 2")
	putU16(b, offIdentity, `{"name":"Toxic Angel.3012","profession":5,"spec":7,"race":2,"map_id":50,"world_id":268435505,"team_color_id":0,"commander":false,"fov":0.873,"uisz":1}`)
	putU16(b, offDescription, "A player character in the video game Guild Wars 2.")

	// context_len stays 48, the value GW2 tells Mumble. Extra fields are past it.
	binary.LittleEndian.PutUint32(b[offContextLen:], 48)
	c := b[offContext:]
	binary.LittleEndian.PutUint32(c[28:], 50)     // map
	binary.LittleEndian.PutUint32(c[32:], 5)      // public
	binary.LittleEndian.PutUint32(c[36:], 1009)   // shard
	binary.LittleEndian.PutUint32(c[40:], 3)      // instance
	binary.LittleEndian.PutUint32(c[44:], 165123) // build
	binary.LittleEndian.PutUint32(c[48:], 1|8|64) // map open, focus, combat
	binary.LittleEndian.PutUint16(c[52:], 400)
	binary.LittleEndian.PutUint16(c[54:], 400)
	putF32(c, 56, 1.25)
	putF32(c, 60, 1234.5)
	putF32(c, 64, 5678.5)
	putF32(c, 68, 1000)
	putF32(c, 72, 2000)
	putF32(c, 76, 8)
	binary.LittleEndian.PutUint32(c[80:], 4242)
	c[84] = 8 // skyscale
	return b
}

func TestFrameReady(t *testing.T) {
	if FrameReady(nil) {
		t.Fatal("nil")
	}
	zeros := make([]byte, LinkSize)
	if FrameReady(zeros) {
		t.Fatal("zeros")
	}
	if !FrameReady(sampleFrame()) {
		t.Fatal("sample")
	}
}

func TestDecodeSample(t *testing.T) {
	s, err := Decode(sampleFrame())
	if err != nil {
		t.Fatal(err)
	}
	if s.Tick != 9001 || s.Version != 2 {
		t.Fatalf("tick/version: %+v", s)
	}
	if s.Name != "Guild Wars 2" {
		t.Fatalf("name: %q", s.Name)
	}
	if s.AvatarPos == nil || s.AvatarPos[0] != 10 || s.AvatarPos[2] != 30 {
		t.Fatalf("avatar pos: %v", s.AvatarPos)
	}
	if s.AvatarFront != nil {
		t.Fatal("NaN front should be dropped")
	}
	if s.ContextLen != 48 {
		t.Fatalf("context_len: %d", s.ContextLen)
	}
	if s.MapID != 50 || s.MapType != 5 || s.ShardID != 1009 || s.BuildID != 165123 {
		t.Fatalf("context ids: %+v", s)
	}
	if !s.MapOpen || !s.GameFocus || !s.InCombat || s.TextFocus || s.Competitive {
		t.Fatalf("ui bits: %+v", s)
	}
	if s.Mount != "Skyscale" || s.MountIndex != 8 {
		t.Fatalf("mount: %s %d", s.Mount, s.MountIndex)
	}
	if s.Character != "Toxic Angel.3012" || s.Profession != "Thief" || s.Race != "Human" {
		t.Fatalf("identity: %s %s %s", s.Character, s.Profession, s.Race)
	}
	if s.ProcessID != 4242 {
		t.Fatalf("pid: %d", s.ProcessID)
	}
	if s.PlayerX == nil || *s.PlayerX != 1234.5 {
		t.Fatalf("playerX: %v", s.PlayerX)
	}
}

func TestShortBuffer(t *testing.T) {
	_, err := Decode(bytes.Repeat([]byte{0}, 100))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMountNames(t *testing.T) {
	b := make([]byte, LinkSize)
	b[offContext+84] = 5
	s, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if s.Mount != "Raptor" {
		t.Fatalf("got %s", s.Mount)
	}
	b[offContext+84] = 99
	s, err = Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if s.Mount != "unknown(99)" {
		t.Fatalf("got %s", s.Mount)
	}
}
