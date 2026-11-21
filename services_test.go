package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// Fixtures are real TDs read on the LAN (2026-10-05): none carries @type.
func loadTD(t *testing.T, name string) TD {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var td TD
	if err := json.Unmarshal(b, &td); err != nil {
		t.Fatal(err)
	}
	return td
}

func TestBuildServices(t *testing.T) {
	cases := []struct {
		fixture string
		label   string
		want    []string // capabilities, in order
	}{
		{"lampe", "Appliques Séjour", []string{"LightDimmer"}},
		{"volet", "Volet Séjour", []string{"ShutterPosition"}},
		{"garage", "Porte Garage", []string{"GarageDoor"}},
		{"sonnette", "Sonnette", []string{"Trigger"}},
		{"eau", "Eau Chaude", []string{"WaterMeter"}},
		{"contexts", "Context", []string{"Contexts"}},
		{"weather", "Weather", []string{"BarometerSensor", "HygrometerSensor", "TemperatureSensor", "UVSensor", "WindgaugeSensor"}},
		{"tic", "Teleinfo", []string{genericCapability}},
	}
	for _, c := range cases {
		t.Run(c.fixture, func(t *testing.T) {
			var got []string
			for _, s := range buildServices(loadTD(t, c.fixture), true) {
				got = append(got, s.Capability)
				if s.Label != c.label {
					t.Errorf("label = %q, want %q", s.Label, c.label)
				}
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("capabilities = %v, want %v", got, c.want)
			}
		})
	}
}

func TestWritable(t *testing.T) {
	lamp, garage, eau := loadTD(t, "lampe"), loadTD(t, "garage"), loadTD(t, "eau")
	for _, c := range []struct {
		td   TD
		prop string
		want bool
	}{
		{lamp, "on", false},         // state only: use the toggle action
		{lamp, "brightness", false}, // state only: use the fade action
		{garage, "open", false},
		{loadTD(t, "tic"), "raw", false}, // generic, readOnly in the TD
		{eau, "liters", false},           // sensor value, writable in the TD
		{lamp, "missing", false},
	} {
		if got := isWritable(c.td, c.prop); got != c.want {
			t.Errorf("%s.%s writable = %v, want %v", c.td.ID, c.prop, got, c.want)
		}
	}
}

func TestHref(t *testing.T) {
	p := loadTD(t, "volet").Properties["position"]
	if got := href(p.Forms, "writeproperty"); got != "http://10.0.0.33:20001/voletSEJ/position" {
		t.Errorf("href = %q", got)
	}
	if got := href(p.Forms, "observeproperty"); got != "ws://10.0.0.33:20001/voletSEJ/position" {
		t.Errorf("observe href = %q", got)
	}
}

// manager-contexts' "chosen": the named choices reach the UI (eria-choice).
func TestChoicesRelayed(t *testing.T) {
	var td TD
	json.Unmarshal([]byte(`{"id":"urn:c","properties":{"chosen":{"type":"array","items":{"type":"string","oneOf":[{"const":"away","title":"Absents"}]}}}}`), &td)
	b, _ := json.Marshal(buildServices(td, true)[0].Properties["chosen"])
	if !strings.Contains(string(b), `"items":{"type":"string","oneOf":[{"const":"away","title":"Absents"}]}`) {
		t.Errorf("chosen = %s", b)
	}
}

// A camera without @type (gateway-reolink): its images, snapshot and alarm make
// one Camera service; the streams stay generic.
func TestCameraService(t *testing.T) {
	var td TD
	raw := `{"id":"urn:cam","title":"Entrée",
		"properties":{"latest":{"type":"string","contentMediaType":"image/jpeg"},"latestMain":{"type":"string"},"streams":{"type":"object"}},
		"actions":{"snapshot":{}},"events":{"alarm":{"title":"Alarm"}}}`
	if err := json.Unmarshal([]byte(raw), &td); err != nil {
		t.Fatal(err)
	}
	services := buildServices(td, true)
	if len(services) != 2 || services[0].Capability != "Camera" || services[1].Capability != genericCapability {
		t.Fatalf("services = %+v", services)
	}
	cam := services[0]
	if _, ok := cam.Events["alarm"]; !ok {
		t.Error("alarm not in the Camera service")
	}
	if _, ok := cam.Actions["snapshot"]; !ok {
		t.Error("snapshot not in the Camera service")
	}
	if _, ok := services[1].Properties["streams"]; !ok || len(services[1].Events) != 0 {
		t.Errorf("generic = %+v", services[1])
	}
}
