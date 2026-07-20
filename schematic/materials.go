package schematic

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed catalog.json
var catalogJSON []byte

type catalogFile struct {
	MinecraftVersion string     `json:"minecraft_version"`
	DataVersion      int        `json:"data_version"`
	Materials        []Material `json:"materials"`
}

type Material struct {
	ID     string `json:"id"`
	Hex    string `json:"hex"`
	Cost   int    `json:"cost"`
	Legacy string `json:"legacy"`
	Auto   bool   `json:"auto"`
	lab    lab
}

type ResolvedMaterial struct {
	ID, Legacy, Requested string
	Hex                   string
	Cost                  int
	Distance              float64
}

type Catalog struct {
	MinecraftVersion string
	DataVersion      int
	materials        []Material
	byID             map[string]Material
}

var catalogOnce = sync.OnceValue(func() *Catalog {
	var file catalogFile
	if err := json.Unmarshal(catalogJSON, &file); err != nil {
		panic(err)
	}
	c := &Catalog{MinecraftVersion: file.MinecraftVersion, DataVersion: file.DataVersion, materials: file.Materials, byID: make(map[string]Material, len(file.Materials))}
	for i := range c.materials {
		m := &c.materials[i]
		m.ID = normalizeID(m.ID)
		m.Legacy = normalizeID(m.Legacy)
		rgb, _ := parseHex(m.Hex)
		m.lab = rgbToLab(rgb)
		c.byID[m.ID] = *m
	}
	return c
})

func DefaultCatalog() *Catalog { return catalogOnce() }

func normalizeID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "air" {
		return "minecraft:air"
	}
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	return id
}

func (c *Catalog) Resolve(value string) (ResolvedMaterial, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "#") {
		rgb, err := parseHex(value)
		if err != nil {
			return ResolvedMaterial{}, err
		}
		target := rgbToLab(rgb)
		bestScore := math.Inf(1)
		var best Material
		bestDist := 0.0
		for _, m := range c.materials {
			if !m.Auto {
				continue
			}
			d := deltaE2000(target, m.lab)
			score := d + 3*float64(m.Cost)
			if score < bestScore-1e-9 || math.Abs(score-bestScore) < 1e-9 && (m.Cost < best.Cost || m.Cost == best.Cost && m.ID < best.ID) {
				bestScore, best, bestDist = score, m, d
			}
		}
		if best.ID == "" {
			return ResolvedMaterial{}, fmt.Errorf("no automatic color materials available")
		}
		return ResolvedMaterial{ID: best.ID, Legacy: best.Legacy, Requested: strings.ToUpper(value), Hex: best.Hex, Cost: best.Cost, Distance: bestDist}, nil
	}
	id := normalizeID(value)
	m, ok := c.byID[id]
	if !ok {
		return ResolvedMaterial{}, fmt.Errorf("unknown or unsupported block %q", value)
	}
	return ResolvedMaterial{ID: m.ID, Legacy: m.Legacy, Requested: value, Hex: m.Hex, Cost: m.Cost}, nil
}

func (c *Catalog) Material(id string) (Material, bool) {
	m, ok := c.byID[normalizeID(id)]
	return m, ok
}
func (c *Catalog) PromptMaterials() string {
	ids := make([]string, 0, len(c.materials))
	for _, m := range c.materials {
		if m.ID != "minecraft:air" && (m.Auto || m.ID == "minecraft:glass") {
			ids = append(ids, strings.TrimPrefix(m.ID, "minecraft:"))
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ", ")
}

type rgb struct{ r, g, b float64 }
type lab struct{ l, a, b float64 }

func parseHex(s string) (rgb, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return rgb{}, fmt.Errorf("color must be #RRGGBB")
	}
	v, err := strconv.ParseUint(s, 16, 24)
	if err != nil {
		return rgb{}, fmt.Errorf("invalid color #%s", s)
	}
	return rgb{float64(v>>16&255) / 255, float64(v>>8&255) / 255, float64(v&255) / 255}, nil
}
func linear(v float64) float64 {
	if v <= .04045 {
		return v / 12.92
	}
	return math.Pow((v+.055)/1.055, 2.4)
}
func rgbToLab(c rgb) lab {
	r, g, b := linear(c.r), linear(c.g), linear(c.b)
	x := (.4124564*r + .3575761*g + .1804375*b) / .95047
	y := (.2126729*r + .7151522*g + .072175*b)
	z := (.0193339*r + .119192*g + .9503041*b) / 1.08883
	f := func(v float64) float64 {
		if v > .008856 {
			return math.Cbrt(v)
		}
		return 7.787*v + 16.0/116
	}
	fx, fy, fz := f(x), f(y), f(z)
	return lab{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// deltaE2000 implements the CIEDE2000 perceptual color difference formula.
func deltaE2000(x, y lab) float64 {
	c1, c2 := math.Hypot(x.a, x.b), math.Hypot(y.a, y.b)
	cm := (c1 + c2) / 2
	g := .5 * (1 - math.Sqrt(math.Pow(cm, 7)/(math.Pow(cm, 7)+math.Pow(25, 7))))
	a1, a2 := (1+g)*x.a, (1+g)*y.a
	cp1, cp2 := math.Hypot(a1, x.b), math.Hypot(a2, y.b)
	h := func(a, b float64) float64 {
		v := math.Atan2(b, a) * 180 / math.Pi
		if v < 0 {
			v += 360
		}
		return v
	}
	h1, h2 := h(a1, x.b), h(a2, y.b)
	dl := y.l - x.l
	dc := cp2 - cp1
	dh := h2 - h1
	if cp1*cp2 == 0 {
		dh = 0
	} else if dh > 180 {
		dh -= 360
	} else if dh < -180 {
		dh += 360
	}
	dH := 2 * math.Sqrt(cp1*cp2) * math.Sin(dh*math.Pi/360)
	lm := (x.l + y.l) / 2
	cpm := (cp1 + cp2) / 2
	hm := h1 + h2
	if cp1*cp2 == 0 {
		hm = h1 + h2
	} else if math.Abs(h1-h2) <= 180 {
		hm = (h1 + h2) / 2
	} else if h1+h2 < 360 {
		hm = (h1 + h2 + 360) / 2
	} else {
		hm = (h1 + h2 - 360) / 2
	}
	t := 1 - .17*math.Cos((hm-30)*math.Pi/180) + .24*math.Cos(2*hm*math.Pi/180) + .32*math.Cos((3*hm+6)*math.Pi/180) - .20*math.Cos((4*hm-63)*math.Pi/180)
	sl := 1 + .015*(lm-50)*(lm-50)/math.Sqrt(20+(lm-50)*(lm-50))
	sc := 1 + .045*cpm
	sh := 1 + .015*cpm*t
	rt := -2 * math.Sqrt(math.Pow(cpm, 7)/(math.Pow(cpm, 7)+math.Pow(25, 7))) * math.Sin(60*math.Exp(-math.Pow((hm-275)/25, 2))*math.Pi/180)
	return math.Sqrt(math.Pow(dl/sl, 2) + math.Pow(dc/sc, 2) + math.Pow(dH/sh, 2) + rt*(dc/sc)*(dH/sh))
}
