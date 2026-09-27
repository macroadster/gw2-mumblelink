package mumblelink

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestClientWhere(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/v2/maps/15":
			w.Write([]byte(`{
				"id": 15, "name": "Queensdale", "continent_id": 1, "continent_name": "Tyria",
				"region_id": 4, "region_name": "Kryta", "default_floor": 1, "floors": [1],
				"map_rect": [[0,0],[2400,2400]],
				"continent_rect": [[0,0],[100,100]]
			}`))
		case "/v2/continents/1/floors/1/regions/4/maps/15":
			w.Write([]byte(`{
				"points_of_interest": {
					"1": {"name": "North Gate", "type": "waypoint", "coord": [0, -100], "chat_link": "[&AAA=]"},
					"2": {"name": "Field", "type": "landmark", "coord": [10, -10]}
				},
				"tasks": {"9": {"objective": "Help the village", "coord": [50, 0], "chat_link": "[&BBB=]"}},
				"skill_challenges": [{"coord": [5, 5]}],
				"sectors": {"3": {"name": "Village", "coord": [1, -1]}}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.Base = srv.URL
	c.CacheDir = t.TempDir()
	front := [3]float32{0, 0, 1}
	s := Snapshot{MapID: 15, PlayerX: f32p(0), PlayerY: f32p(0), AvatarFront: &front, Character: "Tester"}
	place, err := c.Where(context.Background(), s, 8)
	if err != nil {
		t.Fatal(err)
	}
	if place.MapName != "Queensdale" || place.Area != "Village" {
		t.Fatalf("place: %+v", place)
	}
	if place.NearestWaypoint == nil || place.NearestWaypoint.Name != "North Gate" || place.NearestWaypoint.Turn != "ahead" {
		t.Fatalf("waypoint: %+v", place.NearestWaypoint)
	}
	if place.Hint == "" || place.Facing != "north" {
		t.Fatalf("hint %q facing %s", place.Hint, place.Facing)
	}
	// Kinds we asked for are present. Order is by distance.
	kinds := map[string]bool{}
	for _, h := range place.Nearby {
		kinds[h.Kind] = true
	}
	for _, k := range []string{"waypoint", "landmark", "heart", "hero_point"} {
		if !kinds[k] {
			t.Fatalf("missing %s in %+v", k, place.Nearby)
		}
	}
	n := hits.Load()
	if _, err := c.Where(context.Background(), s, 8); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != n {
		t.Fatalf("cache missed: hits %d then %d", n, hits.Load())
	}
}
