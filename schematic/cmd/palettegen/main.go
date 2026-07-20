// Command palettegen regenerates schematic/catalog.json from official
// Minecraft Java client assets. Only derived colors and block names are stored.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type versionManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []struct{ ID, URL, Type string } `json:"versions"`
}
type versionMeta struct {
	Downloads struct {
		Client struct {
			URL string `json:"url"`
		} `json:"client"`
	} `json:"downloads"`
}
type versionInfo struct {
	ID           string `json:"id"`
	WorldVersion int    `json:"world_version"`
	Stable       bool   `json:"stable"`
}
type material struct {
	ID     string `json:"id"`
	Hex    string `json:"hex"`
	Cost   int    `json:"cost"`
	Legacy string `json:"legacy"`
	Auto   bool   `json:"auto"`
}
type catalog struct {
	MinecraftVersion string     `json:"minecraft_version"`
	DataVersion      int        `json:"data_version"`
	Materials        []material `json:"materials"`
}
type blockState struct {
	Variants map[string]struct {
		Model string `json:"model"`
	} `json:"variants"`
	Multipart []struct {
		Apply json.RawMessage `json:"apply"`
	} `json:"multipart"`
}
type blockModel struct {
	Parent   string            `json:"parent"`
	Textures map[string]string `json:"textures"`
}

func main() {
	out := flag.String("out", "schematic/catalog.json", "catalog output path")
	seed := flag.String("seed", "schematic/catalog.json", "curated catalog whose costs/legacy/auto flags are preserved")
	cache := flag.String("cache", "", "client jar cache (defaults to a versioned file in the system temp directory)")
	version := flag.String("version", "latest", "release ID or latest")
	flag.Parse()
	client := &http.Client{Timeout: 2 * time.Minute}
	var manifest versionManifest
	mustJSON(client, "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest)
	target := *version
	if target == "latest" {
		target = manifest.Latest.Release
	}
	if *cache == "" {
		*cache = filepath.Join(os.TempDir(), "x3-minecraft-client-"+target+".jar")
	}
	metaURL := ""
	for _, v := range manifest.Versions {
		if v.ID == target {
			metaURL = v.URL
			break
		}
	}
	if metaURL == "" {
		panic("unknown Minecraft release " + target)
	}
	var meta versionMeta
	mustJSON(client, metaURL, &meta)
	if _, err := os.Stat(*cache); err != nil {
		fmt.Println("downloading", target, "client assets...")
		resp, err := client.Get(meta.Downloads.Client.URL)
		must(err)
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			panic(resp.Status)
		}
		tmp := *cache + ".tmp"
		f, err := os.Create(tmp)
		must(err)
		_, err = io.Copy(f, resp.Body)
		closeErr := f.Close()
		must(err)
		must(closeErr)
		must(os.Rename(tmp, *cache))
	}
	zr, err := zip.OpenReader(*cache)
	must(err)
	defer zr.Close()
	entries := map[string]*zip.File{}
	for _, f := range zr.File {
		entries[f.Name] = f
	}
	var vi versionInfo
	must(readJSON(entries["version.json"], &vi))
	if vi.ID != target || !vi.Stable {
		panic(fmt.Sprintf("cached jar is %s (stable=%t), wanted %s", vi.ID, vi.Stable, target))
	}
	curated := map[string]material{}
	var old catalog
	if data, err := os.ReadFile(*seed); err == nil && json.Unmarshal(data, &old) == nil {
		for _, m := range old.Materials {
			curated[m.ID] = m
		}
	}
	legacyChoices := buildLegacyChoices(old.Materials)
	resolver := modelResolver{entries: entries}
	materials := make([]material, 0, 1500)
	prefix := "assets/minecraft/blockstates/"
	for name := range entries {
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := "minecraft:" + strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
		hex := resolver.blockColor(name)
		m, ok := curated[id]
		if !ok {
			m = material{ID: id, Hex: hex, Cost: 2, Auto: false}
		} else if hex != "" {
			m.Hex = hex
		}
		if m.Hex == "" {
			m.Hex = "#7D7D7D"
		}
		if m.Legacy == "" || m.Legacy == "minecraft:stone" && id != "minecraft:stone" {
			m.Legacy = chooseLegacy(id, m.Hex, legacyChoices)
		}
		materials = append(materials, m)
	}
	sort.Slice(materials, func(i, j int) bool { return materials[i].ID < materials[j].ID })
	generated := catalog{MinecraftVersion: vi.ID, DataVersion: vi.WorldVersion, Materials: materials}
	data, err := json.MarshalIndent(generated, "", "  ")
	must(err)
	data = append(data, '\n')
	must(os.WriteFile(*out, data, 0644))
	fmt.Printf("wrote %d blocks for Java %s (DataVersion %d) to %s\n", len(materials), vi.ID, vi.WorldVersion, *out)
}

type modelResolver struct{ entries map[string]*zip.File }

type legacyChoice struct {
	id   string
	hex  string
	cost int
}

func buildLegacyChoices(materials []material) []legacyChoice {
	byID := make(map[string]material, len(materials))
	for _, m := range materials {
		byID[m.ID] = m
	}
	unique := map[string]legacyChoice{}
	for _, m := range materials {
		if !m.Auto && m.ID != "minecraft:glass" || m.Legacy == "" || m.Legacy == "minecraft:air" {
			continue
		}
		hex := m.Hex
		if target, ok := byID[m.Legacy]; ok && target.Hex != "" {
			hex = target.Hex
		}
		unique[m.Legacy] = legacyChoice{id: m.Legacy, hex: hex, cost: m.Cost}
	}
	out := make([]legacyChoice, 0, len(unique))
	for _, choice := range unique {
		out = append(out, choice)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func chooseLegacy(id, hex string, choices []legacyChoice) string {
	if strings.Contains(id, "glass") {
		return "minecraft:glass"
	}
	filtered := choices
	if containsAny(id, "planks", "log", "wood", "stem", "hyphae", "door", "trapdoor", "fence", "sign", "shelf") {
		filtered = make([]legacyChoice, 0, 6)
		for _, choice := range choices {
			if strings.HasSuffix(choice.id, "_planks") {
				filtered = append(filtered, choice)
			}
		}
	}
	if len(filtered) == 0 {
		filtered = choices
	}
	target, ok := hexRGB(hex)
	if !ok || len(filtered) == 0 {
		return "minecraft:stone"
	}
	best, bestScore := "minecraft:stone", int64(1<<62)
	for _, choice := range filtered {
		color, ok := hexRGB(choice.hex)
		if !ok {
			continue
		}
		dr, dg, db := int64(target[0]-color[0]), int64(target[1]-color[1]), int64(target[2]-color[2])
		score := 3*dr*dr + 4*dg*dg + 2*db*db + int64(choice.cost*choice.cost*100)
		if score < bestScore || score == bestScore && choice.id < best {
			best, bestScore = choice.id, score
		}
	}
	return best
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func hexRGB(value string) ([3]int, bool) {
	var out [3]int
	if len(value) != 7 || value[0] != '#' {
		return out, false
	}
	_, err := fmt.Sscanf(value, "#%02X%02X%02X", &out[0], &out[1], &out[2])
	return out, err == nil
}

func (r modelResolver) blockColor(statePath string) string {
	var state blockState
	if readJSON(r.entries[statePath], &state) != nil {
		return ""
	}
	models := []string{}
	for _, v := range state.Variants {
		models = append(models, v.Model)
		break
	}
	if len(models) == 0 && len(state.Multipart) > 0 {
		var one struct {
			Model string `json:"model"`
		}
		if json.Unmarshal(state.Multipart[0].Apply, &one) == nil && one.Model != "" {
			models = append(models, one.Model)
		} else {
			var many []struct {
				Model string `json:"model"`
			}
			if json.Unmarshal(state.Multipart[0].Apply, &many) == nil && len(many) > 0 {
				models = append(models, many[0].Model)
			}
		}
	}
	colors := []pixel{}
	for _, name := range models {
		textures := r.modelTextures(name, 0, map[string]string{})
		for _, tex := range textures {
			tex = strings.TrimPrefix(tex, "minecraft:")
			if !strings.HasPrefix(tex, "block/") {
				continue
			}
			if p, ok := averagePNG(r.entries["assets/minecraft/textures/"+tex+".png"]); ok {
				colors = append(colors, p)
			}
		}
	}
	if len(colors) == 0 {
		return ""
	}
	var rr, gg, bb float64
	for _, c := range colors {
		rr += c.r
		gg += c.g
		bb += c.b
	}
	return fmt.Sprintf("#%02X%02X%02X", int(rr/float64(len(colors))+.5), int(gg/float64(len(colors))+.5), int(bb/float64(len(colors))+.5))
}
func (r modelResolver) modelTextures(name string, depth int, inherited map[string]string) map[string]string {
	if depth > 12 {
		return inherited
	}
	name = strings.TrimPrefix(name, "minecraft:")
	var m blockModel
	if readJSON(r.entries["assets/minecraft/models/"+name+".json"], &m) != nil {
		return inherited
	}
	all := map[string]string{}
	for k, v := range inherited {
		all[k] = v
	}
	if m.Parent != "" {
		all = r.modelTextures(m.Parent, depth+1, all)
	}
	for k, v := range m.Textures {
		all[k] = v
	}
	resolve := func(v string) string {
		for seen := 0; strings.HasPrefix(v, "#") && seen < 16; seen++ {
			next, ok := all[strings.TrimPrefix(v, "#")]
			if !ok {
				return ""
			}
			v = next
		}
		return v
	}
	out := map[string]string{}
	for k, v := range all {
		if v = resolve(v); v != "" {
			out[k] = v
		}
	}
	return out
}

type pixel struct{ r, g, b float64 }

func averagePNG(f *zip.File) (pixel, bool) {
	if f == nil {
		return pixel{}, false
	}
	rc, err := f.Open()
	if err != nil {
		return pixel{}, false
	}
	defer rc.Close()
	img, _, err := image.Decode(rc)
	if err != nil {
		return pixel{}, false
	}
	b := img.Bounds()
	var r, g, bl, w float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			rr, gg, bb, aa := img.At(x, y).RGBA()
			if aa == 0 {
				continue
			}
			weight := float64(aa) / 65535
			r += float64(rr) / 257 * weight
			g += float64(gg) / 257 * weight
			bl += float64(bb) / 257 * weight
			w += weight
		}
	}
	if w == 0 {
		return pixel{}, false
	}
	return pixel{r / w, g / w, bl / w}, true
}
func mustJSON(c *http.Client, url string, out any) {
	resp, err := c.Get(url)
	must(err)
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		panic(resp.Status)
	}
	must(json.NewDecoder(resp.Body).Decode(out))
}
func readJSON(f *zip.File, out any) error {
	if f == nil {
		return fmt.Errorf("missing zip entry")
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return json.NewDecoder(rc).Decode(out)
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
