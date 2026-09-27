package mumblelink

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const inchesPerMeter = 39.37007874015748

// Point is one named place on a continent floor, in continent coordinates.
type Point struct {
	Kind     string
	Name     string
	ChatLink string
	X        float64
	Y        float64
}

// MapInfo is the slice of /v2/maps needed to name a zone and scale distances.
type MapInfo struct {
	ID            int           `json:"id"`
	Name          string        `json:"name"`
	ContinentID   int           `json:"continent_id"`
	ContinentName string        `json:"continent_name"`
	RegionID      int           `json:"region_id"`
	RegionName    string        `json:"region_name"`
	DefaultFloor  int           `json:"default_floor"`
	Floors        []int         `json:"floors"`
	MapRect       [2][2]float64 `json:"map_rect"`
	ContinentRect [2][2]float64 `json:"continent_rect"`
}

// Hit is a landmark relative to the player.
// TurnDeg is positive to the right and negative to the left.
// Distance is straight-line, not a path around terrain.
type Hit struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	DistanceM int    `json:"distance_m"`
	Compass   string `json:"compass"`
	Turn      string `json:"turn"`
	TurnDeg   int    `json:"turn_deg"`
	ChatLink  string `json:"chat_link,omitempty"`
}

// Place is where the character is and which landmarks are nearest.
type Place struct {
	MapName         string `json:"map_name"`
	RegionName      string `json:"region_name"`
	ContinentName   string `json:"continent_name"`
	Area            string `json:"area,omitempty"`
	Character       string `json:"character,omitempty"`
	Mount           string `json:"mount,omitempty"`
	Facing          string `json:"facing,omitempty"`
	FacingDeg       int    `json:"facing_deg"`
	NearestWaypoint *Hit   `json:"nearest_waypoint,omitempty"`
	Nearby          []Hit  `json:"nearby"`
	Hint            string `json:"hint"`
}

// Locate ranks points against a live snapshot.
// Continent position comes from player_x and player_y. Facing comes from the
// avatar front vector: +X is east, +Z is north. Continent +Y is south.
func Locate(s Snapshot, info MapInfo, points []Point, limit int) (Place, error) {
	if s.PlayerX == nil || s.PlayerY == nil {
		return Place{}, fmt.Errorf("mumblelink: no continent position. Competitive maps do not update player_x and player_y")
	}
	if limit <= 0 {
		limit = 8
	}
	px := float64(*s.PlayerX)
	py := float64(*s.PlayerY)
	scale := metersPerContinentUnit(info.MapRect, info.ContinentRect)
	facing, hasFacing := facingDeg(s.AvatarFront)

	place := Place{
		MapName:       info.Name,
		RegionName:    info.RegionName,
		ContinentName: info.ContinentName,
		Character:     s.Character,
		Mount:         s.Mount,
	}
	if hasFacing {
		place.FacingDeg = int(math.Round(facing))
		place.Facing = compassLabel(facing)
	}

	var hits []Hit
	var sectors []Hit
	var nearestWP *Hit
	for _, p := range points {
		if p.Name == "" && p.Kind != "vista" && p.Kind != "hero_point" {
			continue
		}
		name := p.Name
		if name == "" {
			name = kindLabel(p.Kind)
		}
		dx := p.X - px
		dy := p.Y - py
		distUnits := math.Hypot(dx, dy)
		meters := distUnits * scale
		bearing := compassDeg(dx, dy)
		hit := Hit{
			Kind:      p.Kind,
			Name:      name,
			DistanceM: int(math.Round(meters)),
			Compass:   compassLabel(bearing),
			ChatLink:  p.ChatLink,
		}
		if hasFacing {
			turn := wrapDeg(bearing - facing)
			hit.TurnDeg = int(math.Round(turn))
			hit.Turn = turnLabel(turn)
		}
		if p.Kind == "sector" {
			sectors = append(sectors, hit)
			continue
		}
		hits = append(hits, hit)
		if p.Kind == "waypoint" && (nearestWP == nil || hit.DistanceM < nearestWP.DistanceM) {
			cp := hit
			nearestWP = &cp
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].DistanceM == hits[j].DistanceM {
			return hits[i].Name < hits[j].Name
		}
		return hits[i].DistanceM < hits[j].DistanceM
	})
	sort.Slice(sectors, func(i, j int) bool { return sectors[i].DistanceM < sectors[j].DistanceM })
	if len(sectors) > 0 {
		place.Area = sectors[0].Name
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	place.Nearby = hits
	place.NearestWaypoint = nearestWP
	place.Hint = hint(place)
	return place, nil
}

func hint(p Place) string {
	where := p.MapName
	if p.Area != "" {
		where = p.Area + ", " + p.MapName
	}
	if p.RegionName != "" && p.RegionName != p.MapName {
		where += " (" + p.RegionName + ")"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are in %s", where)
	if p.Facing != "" {
		fmt.Fprintf(&b, ", facing %s", p.Facing)
	}
	b.WriteByte('.')
	target := p.NearestWaypoint
	if target == nil && len(p.Nearby) > 0 {
		target = &p.Nearby[0]
	}
	if target == nil {
		b.WriteString(" This map has no landmarks in the public API.")
		return b.String()
	}
	fmt.Fprintf(&b, " Nearest %s is %s, about %s %s.", kindLabel(target.Kind), target.Name, formatDistance(target.DistanceM), target.Compass)
	switch target.Turn {
	case "ahead":
		b.WriteString(" It is ahead of you.")
	case "behind":
		b.WriteString(" It is behind you.")
	case "left", "right":
		deg := target.TurnDeg
		if deg < 0 {
			deg = -deg
		}
		fmt.Fprintf(&b, " Turn about %d° %s.", deg, target.Turn)
	}
	return b.String()
}

func kindLabel(kind string) string {
	switch kind {
	case "waypoint":
		return "waypoint"
	case "landmark":
		return "landmark"
	case "vista":
		return "vista"
	case "heart":
		return "heart"
	case "hero_point":
		return "hero point"
	case "sector":
		return "area"
	default:
		return kind
	}
}

func formatDistance(m int) string {
	if m >= 1000 {
		return fmt.Sprintf("%.1f km", float64(m)/1000)
	}
	return fmt.Sprintf("%d m", m)
}

// metersPerContinentUnit converts a continent-unit length into meters.
// Map rectangles are in inches. Continent rectangles are the same space as
// player_x and player_y. The two axes are averaged when a map is stretched.
func metersPerContinentUnit(mapRect, continentRect [2][2]float64) float64 {
	mapW := math.Abs(mapRect[1][0] - mapRect[0][0])
	mapH := math.Abs(mapRect[1][1] - mapRect[0][1])
	conW := math.Abs(continentRect[1][0] - continentRect[0][0])
	conH := math.Abs(continentRect[1][1] - continentRect[0][1])
	if mapW == 0 || mapH == 0 || conW == 0 || conH == 0 {
		return 0
	}
	inchesPerUnit := ((mapW / conW) + (mapH / conH)) / 2
	return inchesPerUnit / inchesPerMeter
}

// facingDeg is degrees clockwise from north. Avatar +X is east, +Z is north.
func facingDeg(front *[3]float32) (float64, bool) {
	if front == nil {
		return 0, false
	}
	east := float64(front[0])
	north := float64(front[2])
	if east == 0 && north == 0 {
		return 0, false
	}
	return compassDeg(east, -north), true
}

// compassDeg is degrees clockwise from north. dx is east, dy is south.
func compassDeg(dx, dy float64) float64 {
	deg := math.Atan2(dx, -dy) * 180 / math.Pi
	if deg < 0 {
		deg += 360
	}
	return deg
}

func wrapDeg(d float64) float64 {
	for d <= -180 {
		d += 360
	}
	for d > 180 {
		d -= 360
	}
	return d
}

func compassLabel(deg float64) string {
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	names := []string{"north", "northeast", "east", "southeast", "south", "southwest", "west", "northwest"}
	i := int(math.Floor((deg+22.5)/45)) % 8
	return names[i]
}

func turnLabel(turn float64) string {
	a := math.Abs(turn)
	switch {
	case a <= 25:
		return "ahead"
	case a >= 155:
		return "behind"
	case turn < 0:
		return "left"
	default:
		return "right"
	}
}
