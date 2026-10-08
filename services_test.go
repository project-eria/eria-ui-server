package main

import (
	"encoding/json"
	"os"
	"slices"
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
