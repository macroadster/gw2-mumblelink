// Package mumblelink reads the Guild Wars 2 MumbleLink shared block.
//
// The game publishes a 5460-byte little-endian LinkedMem snapshot. This
// package only decodes a byte slice. It does not open the game process,
// inject a DLL, or send input.
//
// On Windows the block is the named file mapping "MumbleLink". A macOS
// Guild Wars 2 install through Wine (Sikarugir, CrossOver, Whisky) keeps
// that name inside the wineserver, so ReadShared launches a small Windows
// reader in the same prefix. Callers can also pass an already-copied buffer.
package mumblelink

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"unicode/utf16"
)

const (
	// LinkSize is the Mumble LinkedMem size GW2 writes.
	LinkSize = 5460

	nameChars        = 256
	identityChars    = 256
	descriptionChars = 2048
	contextBytes     = 256

	offVersion     = 0
	offTick        = 4
	offAvatarPos   = 8
	offAvatarFront = 20
	offAvatarTop   = 32
	offName        = 44
	offCameraPos   = 556
	offCameraFront = 568
	offCameraTop   = 580
	offIdentity    = 592
	offContextLen  = 1104
	offContext     = 1108
	offDescription = 1364
)

// Mount names follow the GW2 wiki mountIndex enum.
// 0 is on foot. Values past Siege Turtle stay numeric.
var mountNames = []string{
	"None",
	"Jackal",
	"Griffon",
	"Springer",
	"Skimmer",
	"Raptor",
	"Roller Beetle",
	"Warclaw",
	"Skyscale",
	"Skiff",
	"Siege Turtle",
}

var professionNames = map[int]string{
	1: "Guardian",
	2: "Warrior",
	3: "Engineer",
	4: "Ranger",
	5: "Thief",
	6: "Elementalist",
	7: "Mesmer",
	8: "Necromancer",
	9: "Revenant",
}

var raceNames = map[int]string{
	0: "Asura",
	1: "Charr",
	2: "Human",
	3: "Norn",
	4: "Sylvari",
}

// Snapshot is one decoded frame. Server address bytes are omitted on purpose.
type Snapshot struct {
	Version     uint32      `json:"version"`
	Tick        uint32      `json:"tick"`
	AvatarPos   *[3]float32 `json:"avatar_position_m"`
	AvatarFront *[3]float32 `json:"avatar_front"`
	AvatarTop   *[3]float32 `json:"avatar_top"`
	CameraPos   *[3]float32 `json:"camera_position_m"`
	CameraFront *[3]float32 `json:"camera_front"`
	CameraTop   *[3]float32 `json:"camera_top"`
	Name        string      `json:"name"`
	Description string      `json:"description"`

	// ContextLen is what Mumble uses to tell shards apart. GW2 currently
	// reports 48, but still writes uiState, compass, continent coords, and
	// mountIndex past that prefix. Those fields are decoded anyway.
	ContextLen uint32 `json:"context_len"`
	MapID      uint32 `json:"map_id"`
	MapType    uint32 `json:"map_type"`
	ShardID    uint32 `json:"shard_id"`
	Instance   uint32 `json:"instance"`
	BuildID    uint32 `json:"build_id"`

	UIState     uint32 `json:"ui_state"`
	MapOpen     bool   `json:"map_open"`
	CompassTop  bool   `json:"compass_top_right"`
	CompassRot  bool   `json:"compass_rotation_enabled"`
	GameFocus   bool   `json:"game_focus"`
	Competitive bool   `json:"competitive"`
	TextFocus   bool   `json:"text_input_focus"`
	InCombat    bool   `json:"in_combat"`

	CompassWidth    uint16   `json:"compass_width"`
	CompassHeight   uint16   `json:"compass_height"`
	CompassRotation float32  `json:"compass_rotation"`
	PlayerX         *float32 `json:"player_x"`
	PlayerY         *float32 `json:"player_y"`
	MapCenterX      *float32 `json:"map_center_x"`
	MapCenterY      *float32 `json:"map_center_y"`
	MapScale        *float32 `json:"map_scale"`
	ProcessID       uint32   `json:"process_id"`
	MountIndex      uint8    `json:"mount_index"`
	Mount           string   `json:"mount"`

	Identity     json.RawMessage `json:"identity,omitempty"`
	Character    string          `json:"character,omitempty"`
	Profession   string          `json:"profession,omitempty"`
	ProfessionID int             `json:"profession_id,omitempty"`
	Race         string          `json:"race,omitempty"`
	RaceID       int             `json:"race_id,omitempty"`
	SpecID       int             `json:"spec_id,omitempty"`
	Commander    bool            `json:"commander,omitempty"`
	FOV          float64         `json:"fov,omitempty"`
}

// FrameReady reports whether buf looks like a frame the game has written.
// Version is 1 or 2 and the tick has moved off zero.
func FrameReady(buf []byte) bool {
	if len(buf) < 8 {
		return false
	}
	ver := binary.LittleEndian.Uint32(buf[offVersion:])
	tick := binary.LittleEndian.Uint32(buf[offTick:])
	return (ver == 1 || ver == 2) && tick > 0
}

// Decode parses one 5460-byte little-endian frame.
// A short buffer is an error. Extra bytes are ignored.
func Decode(buf []byte) (Snapshot, error) {
	if len(buf) < LinkSize {
		return Snapshot{}, fmt.Errorf("mumblelink: need %d bytes, got %d", LinkSize, len(buf))
	}
	b := buf[:LinkSize]
	var s Snapshot
	s.Version = binary.LittleEndian.Uint32(b[offVersion:])
	s.Tick = binary.LittleEndian.Uint32(b[offTick:])
	s.AvatarPos = vec3(b[offAvatarPos:])
	s.AvatarFront = vec3(b[offAvatarFront:])
	s.AvatarTop = vec3(b[offAvatarTop:])
	s.CameraPos = vec3(b[offCameraPos:])
	s.CameraFront = vec3(b[offCameraFront:])
	s.CameraTop = vec3(b[offCameraTop:])
	s.Name = utf16z(b[offName : offName+nameChars*2])
	s.Description = utf16z(b[offDescription : offDescription+descriptionChars*2])

	s.ContextLen = binary.LittleEndian.Uint32(b[offContextLen:])
	idRaw := utf16z(b[offIdentity : offIdentity+identityChars*2])
	decodeContext(&s, b[offContext:offContext+contextBytes])
	decodeIdentity(&s, idRaw)
	return s, nil
}

func decodeContext(s *Snapshot, c []byte) {
	s.MapID = binary.LittleEndian.Uint32(c[28:])
	s.MapType = binary.LittleEndian.Uint32(c[32:])
	s.ShardID = binary.LittleEndian.Uint32(c[36:])
	s.Instance = binary.LittleEndian.Uint32(c[40:])
	s.BuildID = binary.LittleEndian.Uint32(c[44:])
	s.UIState = binary.LittleEndian.Uint32(c[48:])
	s.MapOpen = s.UIState&1 != 0
	s.CompassTop = s.UIState&2 != 0
	s.CompassRot = s.UIState&4 != 0
	s.GameFocus = s.UIState&8 != 0
	s.Competitive = s.UIState&16 != 0
	s.TextFocus = s.UIState&32 != 0
	s.InCombat = s.UIState&64 != 0
	s.CompassWidth = binary.LittleEndian.Uint16(c[52:])
	s.CompassHeight = binary.LittleEndian.Uint16(c[54:])
	s.CompassRotation = f32(c[56:])
	s.PlayerX = f32ptr(c[60:])
	s.PlayerY = f32ptr(c[64:])
	s.MapCenterX = f32ptr(c[68:])
	s.MapCenterY = f32ptr(c[72:])
	s.MapScale = f32ptr(c[76:])
	s.ProcessID = binary.LittleEndian.Uint32(c[80:])
	s.MountIndex = c[84]
	if int(s.MountIndex) < len(mountNames) {
		s.Mount = mountNames[s.MountIndex]
	} else {
		s.Mount = fmt.Sprintf("unknown(%d)", s.MountIndex)
	}
}

type identityJSON struct {
	Name        string  `json:"name"`
	Profession  int     `json:"profession"`
	Spec        int     `json:"spec"`
	Race        int     `json:"race"`
	MapID       int     `json:"map_id"`
	WorldID     int     `json:"world_id"`
	TeamColorID int     `json:"team_color_id"`
	Commander   bool    `json:"commander"`
	FOV         float64 `json:"fov"`
	UISize      int     `json:"uisz"`
}

func decodeIdentity(s *Snapshot, raw string) {
	raw = trimJSON(raw)
	if raw == "" {
		return
	}
	s.Identity = json.RawMessage(raw)
	var id identityJSON
	if err := json.Unmarshal([]byte(raw), &id); err != nil {
		return
	}
	s.Character = id.Name
	s.ProfessionID = id.Profession
	s.Profession = professionNames[id.Profession]
	s.RaceID = id.Race
	s.Race = raceNames[id.Race]
	s.SpecID = id.Spec
	s.Commander = id.Commander
	s.FOV = id.FOV
}

func trimJSON(s string) string {
	s = string(bytes.TrimSpace([]byte(s)))
	if s == "" || s[0] != '{' {
		return ""
	}
	// Identity is a fixed wchar buffer. Stop at the first NUL already
	// stripped by utf16z; also drop trailing junk after the JSON object.
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if inStr {
			if esc {
				esc = false
				continue
			}
			if ch == '\\' {
				esc = true
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i+1]
			}
		}
	}
	return s
}

func vec3(b []byte) *[3]float32 {
	out := [3]float32{f32(b[0:]), f32(b[4:]), f32(b[8:])}
	for _, v := range out {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil
		}
	}
	return &out
}

func f32(b []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(b))
}

func f32ptr(b []byte) *float32 {
	v := f32(b)
	if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
		return nil
	}
	return &v
}

func utf16z(b []byte) string {
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[i*2:])
		if u[i] == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u))
}
