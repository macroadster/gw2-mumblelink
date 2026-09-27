package mumblelink

import (
	"math"
	"testing"
)

func f32p(v float32) *float32 { return &v }

func TestFacingAndTurn(t *testing.T) {
	// Facing due north: +Z.
	front := [3]float32{0, 0, 1}
	deg, ok := facingDeg(&front)
	if !ok || math.Abs(deg) > 0.01 {
		t.Fatalf("north facing: %v %v", deg, ok)
	}
	// Due east of the player, continent +X.
	got := compassDeg(100, 0)
	if math.Abs(got-90) > 0.01 {
		t.Fatalf("east bearing: %v", got)
	}
	info := MapInfo{
		Name:          "Test",
		RegionName:    "Region",
		ContinentName: "Tyria",
		// 2400 inches across 100 continent units = 24 inches per unit.
		MapRect:       [2][2]float64{{0, 0}, {2400, 2400}},
		ContinentRect: [2][2]float64{{0, 0}, {100, 100}},
	}
	s := Snapshot{
		PlayerX:     f32p(0),
		PlayerY:     f32p(0),
		AvatarFront: &front,
		Character:   "Tester",
	}
	// North is -Y in continent space. 100 units * 24/39.37 ≈ 60.96 m.
	points := []Point{{Kind: "waypoint", Name: "North Gate", X: 0, Y: -100, ChatLink: "[&AAA=]"}}
	place, err := Locate(s, info, points, 8)
	if err != nil {
		t.Fatal(err)
	}
	if place.Facing != "north" {
		t.Fatalf("facing: %s", place.Facing)
	}
	if place.NearestWaypoint == nil {
		t.Fatal("missing waypoint")
	}
	wp := place.NearestWaypoint
	if wp.Compass != "north" || wp.Turn != "ahead" {
		t.Fatalf("hit: %+v", wp)
	}
	if wp.DistanceM < 60 || wp.DistanceM > 62 {
		t.Fatalf("distance: %d", wp.DistanceM)
	}
	if wp.TurnDeg != 0 {
		t.Fatalf("turn: %d", wp.TurnDeg)
	}

	// East waypoint while facing north is a right turn.
	points = []Point{{Kind: "waypoint", Name: "East Gate", X: 100, Y: 0}}
	place, err = Locate(s, info, points, 8)
	if err != nil {
		t.Fatal(err)
	}
	wp = place.NearestWaypoint
	if wp.Compass != "east" || wp.Turn != "right" || wp.TurnDeg < 85 || wp.TurnDeg > 95 {
		t.Fatalf("east hit: %+v", wp)
	}
}

func TestLocateNeedsContinentPosition(t *testing.T) {
	_, err := Locate(Snapshot{}, MapInfo{Name: "X"}, nil, 4)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompassLabels(t *testing.T) {
	if compassLabel(0) != "north" || compassLabel(45) != "northeast" || compassLabel(359) != "north" {
		t.Fatalf("%s %s %s", compassLabel(0), compassLabel(45), compassLabel(359))
	}
}
