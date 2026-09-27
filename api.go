package mumblelink

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const apiBase = "https://api.guildwars2.com"

// Client loads map names and landmarks from the public Guild Wars 2 API.
// Responses are cached on disk for a day. A failed refresh still uses an
// older cache when one exists.
type Client struct {
	Base     string
	HTTP     *http.Client
	CacheDir string
	TTL      time.Duration
}

func NewClient() *Client {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return &Client{
		Base:     apiBase,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
		CacheDir: filepath.Join(dir, "gw2-mumblelink", "api"),
		TTL:      24 * time.Hour,
	}
}

// Where reads landmarks for the snapshot's map and ranks them.
func (c *Client) Where(ctx context.Context, s Snapshot, limit int) (Place, error) {
	info, err := c.Map(ctx, int(s.MapID))
	if err != nil {
		return Place{}, err
	}
	points, err := c.Points(ctx, info)
	if err != nil {
		return Place{}, err
	}
	return Locate(s, info, points, limit)
}

// Map fetches /v2/maps/{id}.
func (c *Client) Map(ctx context.Context, id int) (MapInfo, error) {
	var info MapInfo
	err := c.getJSON(ctx, fmt.Sprintf("%s/v2/maps/%d", c.base(), id), &info)
	if err != nil {
		return MapInfo{}, err
	}
	if info.Name == "" {
		return MapInfo{}, fmt.Errorf("mumblelink: map %d has no name", id)
	}
	return info, nil
}

// Points loads waypoints, vistas, landmarks, hearts, hero points, and sector
// names for the map. It tries the default floor first, then the other floors.
func (c *Client) Points(ctx context.Context, info MapInfo) ([]Point, error) {
	floors := make([]int, 0, len(info.Floors)+1)
	seen := map[int]bool{}
	add := func(f int) {
		if seen[f] {
			return
		}
		seen[f] = true
		floors = append(floors, f)
	}
	add(info.DefaultFloor)
	for _, f := range info.Floors {
		add(f)
	}
	var best []Point
	var last error
	for _, floor := range floors {
		u := fmt.Sprintf("%s/v2/continents/%d/floors/%d/regions/%d/maps/%d",
			c.base(), info.ContinentID, floor, info.RegionID, info.ID)
		var raw regionMap
		if err := c.getJSON(ctx, u, &raw); err != nil {
			last = err
			continue
		}
		pts := raw.points()
		if len(pts) > len(best) {
			best = pts
		}
		if len(best) > 0 && floor == info.DefaultFloor {
			return best, nil
		}
	}
	if len(best) == 0 && last != nil {
		return nil, last
	}
	return best, nil
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return apiBase
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return 24 * time.Hour
}

func (c *Client) getJSON(ctx context.Context, url string, dest any) error {
	path := ""
	if c.CacheDir != "" {
		path = filepath.Join(c.CacheDir, cacheName(url))
		if b, ok := freshCache(path, c.ttl()); ok {
			if err := json.Unmarshal(b, dest); err == nil {
				return nil
			}
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gw2-mumblelink")
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		if b, ok := anyCache(path); ok && json.Unmarshal(b, dest) == nil {
			return nil
		}
		return fmt.Errorf("mumblelink: %s: %w", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		if b, ok := anyCache(path); ok && json.Unmarshal(b, dest) == nil {
			return nil
		}
		return fmt.Errorf("mumblelink: %s: HTTP %d", url, resp.StatusCode)
	}
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		_ = os.WriteFile(path, body, 0o644)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("mumblelink: %s: %w", url, err)
	}
	return nil
}

func cacheName(url string) string {
	h := uint64(14695981039346656037)
	for i := 0; i < len(url); i++ {
		h ^= uint64(url[i])
		h *= 1099511628211
	}
	return strconv.FormatUint(h, 16) + ".json"
}

func freshCache(path string, ttl time.Duration) ([]byte, bool) {
	st, err := os.Stat(path)
	if err != nil || time.Since(st.ModTime()) > ttl {
		return nil, false
	}
	b, err := os.ReadFile(path)
	return b, err == nil
}

func anyCache(path string) ([]byte, bool) {
	if path == "" {
		return nil, false
	}
	b, err := os.ReadFile(path)
	return b, err == nil && len(b) > 0
}

type regionMap struct {
	POI     json.RawMessage `json:"points_of_interest"`
	Tasks   json.RawMessage `json:"tasks"`
	Skills  json.RawMessage `json:"skill_challenges"`
	Sectors json.RawMessage `json:"sectors"`
}

type apiPoint struct {
	Name      string      `json:"name"`
	Objective string      `json:"objective"`
	Type      string      `json:"type"`
	Coord     *[2]float64 `json:"coord"`
	ChatLink  string      `json:"chat_link"`
}

func (r regionMap) points() []Point {
	var out []Point
	for _, p := range decodePoints(r.POI) {
		if p.Coord == nil {
			continue
		}
		kind := p.Type
		switch kind {
		case "waypoint", "landmark", "vista":
		default:
			continue
		}
		out = append(out, Point{Kind: kind, Name: p.Name, ChatLink: p.ChatLink, X: p.Coord[0], Y: p.Coord[1]})
	}
	for _, p := range decodePoints(r.Tasks) {
		if p.Coord == nil {
			continue
		}
		out = append(out, Point{Kind: "heart", Name: p.Objective, ChatLink: p.ChatLink, X: p.Coord[0], Y: p.Coord[1]})
	}
	for _, p := range decodePoints(r.Skills) {
		if p.Coord == nil {
			continue
		}
		name := p.Name
		if name == "" {
			name = "Hero point"
		}
		out = append(out, Point{Kind: "hero_point", Name: name, ChatLink: p.ChatLink, X: p.Coord[0], Y: p.Coord[1]})
	}
	for _, p := range decodePoints(r.Sectors) {
		if p.Coord == nil || p.Name == "" {
			continue
		}
		out = append(out, Point{Kind: "sector", Name: p.Name, ChatLink: p.ChatLink, X: p.Coord[0], Y: p.Coord[1]})
	}
	return out
}

func decodePoints(raw json.RawMessage) []apiPoint {
	raw = bytesTrim(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '[' {
		var list []apiPoint
		if json.Unmarshal(raw, &list) != nil {
			return nil
		}
		return list
	}
	var m map[string]apiPoint
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	list := make([]apiPoint, 0, len(m))
	for _, p := range m {
		list = append(list, p)
	}
	return list
}

func bytesTrim(b []byte) []byte {
	i := 0
	j := len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}
