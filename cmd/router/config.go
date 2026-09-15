package main

// What the page needs to know at run time, and where the data came from.
//
// The frontend asks once, at start, instead of carrying a build-time copy of
// the deployment's URLs: the same bundle then serves the laptop and the VM.

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
)

// buildInfo is data/build-info.json, written by the data pipeline. Every field
// is optional: a missing file leaves the dates empty rather than the service
// unstartable.
type buildInfo struct {
	// The pipeline spells them osmExtractDate and satCadastreDate; the short
	// names are read as well, so neither side has to move first.
	OSMDate         string `json:"osmDate"`
	OSMExtractDate  string `json:"osmExtractDate"`
	SATDate         string `json:"satDate"`
	SATCadastreDate string `json:"satCadastreDate"`
	BuildDate       string `json:"buildDate"`
	Region          string `json:"region,omitempty"`
}

// loadBuildInfo reads the file when the data pipeline left one, and falls back
// to the three env vars the image is built with. A deployment without either
// simply shows no dates rather than refusing to start.
func loadBuildInfo(path string) buildInfo {
	var bi buildInfo
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &bi)
	}
	if bi.OSMDate == "" {
		bi.OSMDate = bi.OSMExtractDate
	}
	if bi.SATDate == "" {
		bi.SATDate = bi.SATCadastreDate
	}
	if bi.OSMDate == "" {
		bi.OSMDate = os.Getenv("OSM_DATE")
	}
	if bi.SATDate == "" {
		bi.SATDate = os.Getenv("SAT_DATE")
	}
	if bi.BuildDate == "" {
		bi.BuildDate = os.Getenv("BUILD_DATE")
	}
	return bi
}

// appConfig is GET /api/config.
type appConfig struct {
	Region  string            `json:"region"`
	Public  string            `json:"publicUrl"`
	Styles  map[string]string `json:"styles"`
	Terrain terrainConfig     `json:"terrain"`
	Data    dataDates         `json:"data"`
	// DisclaimerVersion changes when the wording a person accepted changes:
	// the page asks again when it does.
	DisclaimerVersion string `json:"disclaimerVersion"`
	Attribution       string `json:"attribution"`
	Basemap           bool   `json:"basemap"` // whether this server holds the tiles
	MinZoom           int    `json:"minZoom,omitempty"`
	MaxZoom           int    `json:"maxZoom,omitempty"`
}

type terrainConfig struct {
	URL         string `json:"url"`
	Encoding    string `json:"encoding"`
	Attribution string `json:"attribution"`
	MaxZoom     int    `json:"maxZoom"`
}

type dataDates struct {
	OSM   string `json:"osm"`
	SAT   string `json:"sat"`
	Build string `json:"build"`
}

// The terrain stays on the public Terrarium set for now: it is 80 GB of tiles
// nobody should mirror to answer one region's questions.
const (
	defaultTerrainURL  = "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"
	defaultTerrainAttr = "Terrain: Mapzen / AWS Open Data Terrain Tiles"
	defaultAttribution = "© OpenStreetMap contributors · SAT trail cadastre (Provincia autonoma di Trento)"
)

func (s *server) config() appConfig {
	c := appConfig{
		Region: s.region,
		Public: envOr("PUBLIC_URL", ""),
		Styles: map[string]string{
			"light": "/map/styles/light.json",
			"dark":  "/map/styles/dark.json",
		},
		Terrain: terrainConfig{
			URL:         envOr("TERRAIN_URL", defaultTerrainURL),
			Encoding:    "terrarium",
			Attribution: envOr("TERRAIN_ATTRIBUTION", defaultTerrainAttr),
			MaxZoom:     15,
		},
		Data: dataDates{
			OSM:   s.build.OSMDate,
			SAT:   s.build.SATDate,
			Build: s.build.BuildDate,
		},
		DisclaimerVersion: envOr("DISCLAIMER_VERSION", "1"),
		Attribution:       envOr("ATTRIBUTION", defaultAttribution),
		Basemap:           s.pm != nil,
	}
	if s.pm != nil {
		c.MinZoom, c.MaxZoom = s.pm.MinZoom(), s.pm.MaxZoom()
	}
	return c
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, 200, s.config())
}

// terrainHost is the origin the CSP has to allow for the terrain tiles.
func terrainHost(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return ""
	}
	rest := u[i+3:]
	if j := strings.IndexAny(rest, "/{"); j > 0 {
		rest = rest[:j]
	}
	if rest == "" {
		return ""
	}
	return u[:i+3] + rest
}
