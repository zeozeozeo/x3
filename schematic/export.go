package schematic

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/oriumgames/schem/format"
)

type sparseSchematic struct {
	dims                  Bounds
	blocks                map[Point]*format.BlockState
	offset                Point
	dataVersion           int
	version, nativeFormat string
}

func newSparse(build *Build, legacy bool) *sparseSchematic {
	minP, maxP := occupiedBounds(build.Blocks)
	dims := Bounds{maxP.X - minP.X + 1, maxP.Y - minP.Y + 1, maxP.Z - minP.Z + 1}
	blocks := make(map[Point]*format.BlockState, len(build.Blocks))
	cat := DefaultCatalog()
	for p, id := range build.Blocks {
		if legacy {
			if m, ok := cat.Material(id); ok {
				id = m.Legacy
			}
		}
		q := Point{p.X - minP.X, p.Y - minP.Y, p.Z - minP.Z}
		blocks[q] = &format.BlockState{Name: id}
	}
	return &sparseSchematic{dims: dims, blocks: blocks, dataVersion: cat.DataVersion, version: cat.MinecraftVersion, nativeFormat: "sponge_v3"}
}

func occupiedBounds(blocks map[Point]string) (Point, Point) {
	first := true
	var lo, hi Point
	for p := range blocks {
		if first {
			lo, hi, first = p, p, false
			continue
		}
		lo.X = min(lo.X, p.X)
		lo.Y = min(lo.Y, p.Y)
		lo.Z = min(lo.Z, p.Z)
		hi.X = max(hi.X, p.X)
		hi.Y = max(hi.Y, p.Y)
		hi.Z = max(hi.Z, p.Z)
	}
	return lo, hi
}
func (s *sparseSchematic) Dimensions() (int, int, int)          { return s.dims.X, s.dims.Y, s.dims.Z }
func (s *sparseSchematic) Offset() (int, int, int)              { return s.offset.X, s.offset.Y, s.offset.Z }
func (s *sparseSchematic) SetOffset(x, y, z int)                { s.offset = Point{x, y, z} }
func (s *sparseSchematic) Block(x, y, z int) *format.BlockState { return s.blocks[Point{x, y, z}] }
func (s *sparseSchematic) SetBlock(x, y, z int, b *format.BlockState) {
	p := Point{x, y, z}
	if b == nil {
		delete(s.blocks, p)
	} else {
		s.blocks[p] = b
	}
}
func (s *sparseSchematic) BlockEntity(int, int, int) *format.BlockEntity     { return nil }
func (s *sparseSchematic) SetBlockEntity(int, int, int, *format.BlockEntity) {}
func (s *sparseSchematic) Entities() []*format.Entity                        { return nil }
func (s *sparseSchematic) AddEntity(*format.Entity)                          {}
func (s *sparseSchematic) RemoveEntity(*format.Entity)                       {}
func (s *sparseSchematic) Biome(int, int, int) string                        { return "" }
func (s *sparseSchematic) SetBiome(int, int, int, string)                    {}
func (s *sparseSchematic) Metadata() map[string]any                          { return map[string]any{} }
func (s *sparseSchematic) SetMetadata(string, any)                           {}
func (s *sparseSchematic) Format() string                                    { return s.nativeFormat }
func (s *sparseSchematic) DataVersion() int                                  { return s.dataVersion }
func (s *sparseSchematic) SetDataVersion(v int)                              { s.dataVersion = v }
func (s *sparseSchematic) Version() string                                   { return s.version }

type exportedFile struct {
	name string
	data []byte
}

func exportBuild(build *Build, source string, progress ProgressFunc) ([]byte, Bounds, error) {
	modern := newSparse(build, false)
	legacy := newSparse(build, true)
	w, h, l := modern.Dimensions()
	dims := Bounds{w, h, l}
	formats := []struct {
		id, ext string
		schem   format.Schematic
	}{{"sponge_v3", "schem", modern}, {"litematica_v7", "litematic", modern}, {"axiom", "axiom", modern}, {"mcedit", "schematic", legacy}}
	files := make([]exportedFile, 0, len(formats)+2)
	for i, f := range formats {
		if progress != nil {
			progress(Progress{Stage: "exporting", Detail: fmt.Sprintf("%s (%d/%d)", f.id, i+1, len(formats))})
		}
		var buf bytes.Buffer
		if err := format.WriteFormat(&buf, f.id, f.schem); err != nil {
			return nil, Bounds{}, err
		}
		if err := verifyExport(buf.Bytes(), f.id, len(build.Blocks)); err != nil {
			return nil, Bounds{}, err
		}
		files = append(files, exportedFile{"build." + f.ext, buf.Bytes()})
	}
	report := buildReport(build, dims)
	files = append(files, exportedFile{"build.vxl", []byte(source)}, exportedFile{"report.txt", []byte(report)})
	if progress != nil {
		progress(Progress{Stage: "packaging", Detail: "creating ZIP archive"})
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			return nil, Bounds{}, err
		}
		if _, err = w.Write(f.data); err != nil {
			return nil, Bounds{}, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, Bounds{}, err
	}
	return out.Bytes(), dims, nil
}

func verifyExport(data []byte, id string, want int) error {
	s, err := format.ReadFormat(bytes.NewReader(data), id)
	if err != nil {
		return fmt.Errorf("verify %s: %w", id, err)
	}
	w, h, l := s.Dimensions()
	if w < 1 || h < 1 || l < 1 {
		return fmt.Errorf("verify %s: invalid dimensions", id)
	}
	count := 0
	for y := 0; y < h; y++ {
		for z := 0; z < l; z++ {
			for x := 0; x < w; x++ {
				if state := s.Block(x, y, z); !isAirState(state) {
					count++
				}
			}
		}
	}
	if count == 0 {
		return fmt.Errorf("verify %s: export contains no blocks", id)
	}
	if id != "mcedit" && count != want {
		return fmt.Errorf("verify %s: got %d blocks, want %d", id, count, want)
	}
	return nil
}

func isAirState(state *format.BlockState) bool {
	if state == nil {
		return true
	}
	switch state.Name {
	case "air", "minecraft:air", "minecraft:cave_air", "minecraft:void_air":
		return true
	default:
		return false
	}
}

func buildReport(build *Build, dims Bounds) string {
	var b strings.Builder
	cat := DefaultCatalog()
	fmt.Fprintf(&b, "Minecraft Java: %s\nDataVersion: %d\nCanvas: %dx%dx%d\nOccupied bounds: %dx%dx%d\nOccupied blocks: %d\nPrimitive calls: %d\nBlock writes: %d\nLoop iterations: %d\n\nMaterials:\n", cat.MinecraftVersion, cat.DataVersion, build.Bounds.X, build.Bounds.Y, build.Bounds.Z, dims.X, dims.Y, dims.Z, len(build.Blocks), build.Primitives, build.Writes, build.Loops)
	names := make([]string, 0, len(build.Materials))
	for n := range build.Materials {
		if n != "air" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		m := build.Materials[n]
		fmt.Fprintf(&b, "- %s: %s", n, m.ID)
		if m.Hex != "" {
			fmt.Fprintf(&b, " (requested %s, texture %s, dE00 %.2f, cost %d)", m.Requested, m.Hex, m.Distance, m.Cost)
		}
		if m.Legacy != m.ID {
			fmt.Fprintf(&b, "; MCEdit -> %s", m.Legacy)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func DebugArchive(attempts []AttemptError) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	for _, a := range attempts {
		prefix := fmt.Sprintf("attempt-%d", a.Attempt)
		for name, data := range map[string][]byte{prefix + ".vxl": []byte(a.Source), prefix + "-error.txt": []byte(a.Err.Error())} {
			w, err := zw.Create(name)
			if err != nil {
				return nil, err
			}
			if _, err = io.Copy(w, bytes.NewReader(data)); err != nil {
				return nil, err
			}
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
