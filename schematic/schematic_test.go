package schematic

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/oriumgames/schem/format"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

func TestParseBounds(t *testing.T) {
	tests := map[string]Bounds{"": {128, 128, 128}, "16": {16, 16, 16}, "128x64x96": {128, 64, 96}, "320×320×320": {320, 320, 320}}
	for input, want := range tests {
		got, err := ParseBounds(input)
		if err != nil || got != want {
			t.Fatalf("ParseBounds(%q) = %#v, %v; want %#v", input, got, err, want)
		}
	}
	for _, input := range []string{"0", "321", "1x2", "wat", "1x2x400"} {
		if _, err := ParseBounds(input); err == nil {
			t.Errorf("ParseBounds(%q) succeeded", input)
		}
	}
}

func TestReadmeVXLExamplesParse(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(readme), "```vxl\n")
	if len(parts) < 2 {
		t.Fatal("README contains no VXL examples")
	}
	for i, part := range parts[1:] {
		end := strings.Index(part, "```")
		if end < 0 {
			t.Fatalf("VXL example %d has no closing fence", i+1)
		}
		if _, _, err := Parse(part[:end]); err != nil {
			t.Fatalf("README VXL example %d does not parse: %v", i+1, err)
		}
	}
}

func executeTest(t *testing.T, source string, bounds Bounds) *Build {
	t.Helper()
	p, _, err := Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Execute(context.Background(), p, bounds, DefaultCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDSLVariablesLoopsCommentsAndCarving(t *testing.T) {
	source := "```vxl\n# turtle feet\nmat shell = #45a34a\nmat cut = air\nlet y = 2\nfor i = 0..3 {\n b shell (2+i) y 3\n}\nb cut 3 2 3\n```\nignored prose"
	b := executeTest(t, source, Bounds{8, 8, 8})
	if len(b.Blocks) != 3 {
		t.Fatalf("got %d blocks", len(b.Blocks))
	}
	if _, ok := b.Blocks[Point{3, 2, 3}]; ok {
		t.Fatal("air did not carve")
	}
	if b.Loops != 4 {
		t.Fatalf("got %d loops", b.Loops)
	}
}

func TestDSLLoopRangeMayStartWithVariableExpression(t *testing.T) {
	b := executeTest(t, `mat m = stone
let i_x = 2
for xx = i_x..(i_x+7) {
  b m xx 0 0
}`, Bounds{16, 4, 4})
	if len(b.Blocks) != 8 {
		t.Fatalf("got %d blocks, want 8", len(b.Blocks))
	}
}

func TestDSLLenientLetColorMaterial(t *testing.T) {
	b := executeTest(t, "let ad = #52525A\nb ad 1 2 3", Bounds{8, 8, 8})
	if _, ok := b.Blocks[Point{1, 2, 3}]; !ok {
		t.Fatal("let color alias did not define a material")
	}
	if material, ok := b.Materials["ad"]; !ok || material.Requested != "#52525A" {
		t.Fatalf("unexpected material resolution: %#v", material)
	}
}

func TestDSLConditionalsAndComparisons(t *testing.T) {
	source := `mat even = white_concrete
mat odd = black_concrete
for x = 0..3 {
  for y = 0..3 {
    if ((x+y)%2==0) {
      b even x y 0
    } else {
      b odd x y 0
    }
  }
}
if (1 < 2 && 3 >= 3 && !(4 != 4)) {
  b even 0 0 1
}
b odd 1 0 1`
	b := executeTest(t, source, Bounds{8, 8, 8})
	if len(b.Blocks) != 18 {
		t.Fatalf("got %d blocks, want 18", len(b.Blocks))
	}
	if b.Blocks[Point{0, 0, 0}] != "minecraft:white_concrete" || b.Blocks[Point{1, 0, 0}] != "minecraft:black_concrete" {
		t.Fatalf("checkerboard condition produced wrong materials: %#v", b.Blocks)
	}
}

func TestDSLTemplatesPasteWithScopedParameters(t *testing.T) {
	source := `mat m = stone
let x = 7
template column(x z h) {
  for y = 0..h {
    b m x y z
  }
  let local = 99
}
paste column 1 2 3
paste column (x+1) 4 2
b m x 7 7`
	b := executeTest(t, source, Bounds{16, 16, 16})
	if len(b.Blocks) != 8 {
		t.Fatalf("got %d blocks, want 8", len(b.Blocks))
	}
	if _, ok := b.Blocks[Point{7, 7, 7}]; !ok {
		t.Fatal("template parameter overwrote caller variable")
	}
}

func TestDSLTemplateRecursionIsBounded(t *testing.T) {
	program, _, err := Parse(`mat m = stone
template recursive(n) {
  b m n 0 0
  paste recursive (n+1)
}
paste recursive 0`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), program, Bounds{32, 32, 32}, DefaultCatalog()); err == nil || !strings.Contains(err.Error(), "paste nesting") {
		t.Fatalf("expected bounded recursion error, got %v", err)
	}
}

func TestPrimitives(t *testing.T) {
	source := `mat m = stone
box m 1 1 1 3 3 3
hbox m 5 1 1 7 3 3
sph m 10 5 5 2
ell m 14 5 5 2 3 2
ln m 1 10 1 5 10 5
cyl m 8 10 8 12 10 8 1
tube m 16 10 8 19 10 8 2 1`
	b := executeTest(t, source, Bounds{24, 16, 16})
	if len(b.Blocks) < 80 {
		t.Fatalf("unexpectedly sparse build: %d", len(b.Blocks))
	}
	if b.Primitives != 7 {
		t.Fatalf("got %d primitives", b.Primitives)
	}
}

func TestOutOfBoundsAndLoopErrors(t *testing.T) {
	for _, source := range []string{"mat m=stone\nb m 8 0 0", "mat m=stone\nfor i=0..2 step 0 { b m i 0 0 }"} {
		p, _, err := Parse(source)
		if err == nil {
			_, err = Execute(context.Background(), p, Bounds{8, 8, 8}, DefaultCatalog())
		}
		if err == nil {
			t.Fatalf("program succeeded: %s", source)
		}
	}
}

func TestColorResolutionUsesCatalog(t *testing.T) {
	m, err := DefaultCatalog().Resolve("#000000")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "minecraft:black_concrete" && m.ID != "minecraft:black_wool" {
		t.Fatalf("black resolved to %s", m.ID)
	}
	if m.Legacy == "" {
		t.Fatal("missing legacy mapping")
	}
}

func TestGeneratedCatalogHasDiverseValidLegacyFallbacks(t *testing.T) {
	catalog := DefaultCatalog()
	counts := map[string]int{}
	representative := map[string]string{}
	for _, material := range catalog.materials {
		if material.ID == "minecraft:air" {
			continue
		}
		counts[material.Legacy]++
		representative[material.Legacy] = material.ID
	}
	if len(counts) < 30 {
		t.Fatalf("only %d distinct legacy fallbacks", len(counts))
	}
	if counts["minecraft:stone"] > 200 {
		t.Fatalf("stone is still an overly broad fallback for %d blocks", counts["minecraft:stone"])
	}
	build := &Build{Bounds: Bounds{len(representative), 1, 1}, Blocks: make(map[Point]string)}
	i := 0
	for _, id := range representative {
		build.Blocks[Point{i, 0, 0}] = id
		i++
	}
	legacy := newSparse(build, true)
	var encoded bytes.Buffer
	if err := format.WriteFormat(&encoded, "mcedit", legacy); err != nil {
		t.Fatal(err)
	}
	decoded, err := format.ReadFormat(bytes.NewReader(encoded.Bytes()), "mcedit")
	if err != nil {
		t.Fatal(err)
	}
	nonAir := 0
	for x := range len(representative) {
		if !isAirState(decoded.Block(x, 0, 0)) {
			nonAir++
		}
	}
	if nonAir != len(representative) {
		t.Fatalf("MCEdit round trip retained %d/%d fallback materials", nonAir, len(representative))
	}
}

func TestExportAllFormatsAndZip(t *testing.T) {
	b := executeTest(t, "mat m=red_concrete\nbox m 1 2 3 4 5 6", Bounds{16, 16, 16})
	archive, dims, err := exportBuild(b, "mat m=red_concrete\nbox m 1 2 3 4 5 6", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dims != (Bounds{4, 4, 4}) {
		t.Fatalf("dims = %#v", dims)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"build.schem": false, "build.litematic": false, "build.axiom": false, "build.schematic": false, "build.vxl": false, "report.txt": false}
	for _, f := range zr.File {
		if _, ok := want[f.Name]; ok {
			want[f.Name] = true
			if f.UncompressedSize64 == 0 {
				t.Errorf("%s is empty", f.Name)
			}
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing %s", name)
		}
	}
}

func TestSparseExportVerificationIgnoresDecodedAir(t *testing.T) {
	build := &Build{
		Bounds:    Bounds{32, 32, 32},
		Blocks:    map[Point]string{{1, 1, 1}: "minecraft:stone", {30, 30, 30}: "minecraft:red_concrete"},
		Materials: map[string]ResolvedMaterial{},
	}
	modern := newSparse(build, false)
	for _, formatID := range []string{"sponge_v3", "litematica_v7", "axiom"} {
		var encoded bytes.Buffer
		if err := format.WriteFormat(&encoded, formatID, modern); err != nil {
			t.Fatalf("%s write: %v", formatID, err)
		}
		if err := verifyExport(encoded.Bytes(), formatID, 2); err != nil {
			t.Fatalf("%s verify: %v", formatID, err)
		}
	}
}

func TestGeneratorRepairsAndPackages(t *testing.T) {
	calls := 0
	var progress []Progress
	g := &Generator{Complete: func(_ context.Context, brain *llm.Llmer, _ []model.Model, _ persona.InferenceSettings) (string, llm.Usage, error) {
		calls++
		if calls == 1 {
			brain.AddMessage(llm.RoleAssistant, "not vxl", 0)
			return "not vxl", llm.Usage{PromptTokens: 1, ResponseTokens: 1, TotalTokens: 2}, nil
		}
		source := "mat shell = #45a34a\nbox shell 1 1 1 3 3 3"
		brain.AddMessage(llm.RoleAssistant, source, 0)
		return source, llm.Usage{PromptTokens: 2, ResponseTokens: 2, TotalTokens: 4}, nil
	}}
	result, err := g.Generate(context.Background(), Request{Prompt: "tiny turtle", Bounds: Bounds{16, 16, 16}, Models: []model.Model{{Name: "fake"}}}, func(p Progress) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 || calls != 2 {
		t.Fatalf("attempts=%d calls=%d", result.Attempts, calls)
	}
	if result.Usage.TotalTokens != 6 {
		t.Fatalf("usage=%v", result.Usage)
	}
	if len(result.Archive) == 0 {
		t.Fatal("empty archive")
	}
	if len(result.Repairs) != 1 || result.Repairs[0].Stage != "parsing" {
		t.Fatalf("repairs = %#v", result.Repairs)
	}
	foundRepairProgress := false
	for _, p := range progress {
		if p.Stage == "repairing" && strings.Contains(p.Detail, "Attempt 1") {
			foundRepairProgress = true
		}
	}
	if !foundRepairProgress {
		t.Fatalf("missing repair progress: %#v", progress)
	}
}

func TestGeneratorStripsReasoningBeforeParsing(t *testing.T) {
	g := &Generator{Complete: func(_ context.Context, brain *llm.Llmer, _ []model.Model, _ persona.InferenceSettings) (string, llm.Usage, error) {
		answer := "```vxl\nmat shell = lime_concrete\nbox shell 1 1 1 3 3 3\n```"
		brain.AddMessage(llm.RoleAssistant, answer, 0)
		return "<think>Let roof center be around x=55, z=55. I may use prose and punctuation here.</think>\n" + answer, llm.Usage{}, nil
	}}
	result, err := g.Generate(context.Background(), Request{Prompt: "tiny turtle", Bounds: Bounds{16, 16, 16}, Models: []model.Model{{Name: "fake"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 1 || len(result.Repairs) != 0 {
		t.Fatalf("attempts=%d repairs=%#v", result.Attempts, result.Repairs)
	}
}

func TestSystemPromptExplainsArchitecturalQualityAndValidExamples(t *testing.T) {
	prompt := systemPrompt(DefaultCatalog())
	for _, required := range []string{
		"box is a completely FILLED rectangular solid",
		"hbox makes a hollow rectangular shell",
		"First carve all layers of the opening with air",
		"PARSER-VALID TECHNIQUE EXAMPLES",
		"hbox wall 10 1 10 30 14 26 1",
		"for z = 8..28",
		"traversable hollow interior",
		"if condition { ... } else { ... }",
		"template name(a b ...) { ... }",
		"paste framed_window 12 6 9 4 4",
		"there are no general-purpose functions, arrays, randomness, JavaScript, or external tools",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("system prompt is missing %q", required)
		}
	}
	program, _, err := Parse(`mat wall = stone_bricks
mat beam = stripped_oak_log
mat pane = light_blue_stained_glass
mat empty = air
hbox wall 10 1 10 30 14 26 1
box empty 18 1 9 22 7 12
box empty 12 6 9 16 10 12
box pane 13 7 10 15 9 10
for y = 1..14 {
  b beam 10 y 10
  b beam 30 y 10
  b beam 10 y 26
  b beam 30 y 26
}`)
	if err != nil {
		t.Fatalf("architectural prompt example is not valid VXL: %v", err)
	}
	if _, err := Execute(context.Background(), program, Bounds{40, 32, 40}, DefaultCatalog()); err != nil {
		t.Fatalf("architectural prompt example cannot execute: %v", err)
	}
}

func TestGenerationErrorDebugArchive(t *testing.T) {
	g := &Generator{Complete: func(_ context.Context, _ *llm.Llmer, _ []model.Model, _ persona.InferenceSettings) (string, llm.Usage, error) {
		return "bad", llm.Usage{}, nil
	}}
	_, err := g.Generate(context.Background(), Request{Prompt: "x", Bounds: Bounds{16, 16, 16}, Models: []model.Model{{Name: "fake"}}}, nil)
	var ge *GenerationError
	if !errors.As(err, &ge) || len(ge.Attempts) != MaxAttempts {
		t.Fatalf("error=%v", err)
	}
	data, err := DebugArchive(ge.Attempts)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != MaxAttempts*2 {
		t.Fatalf("got %d files", len(zr.File))
	}
	r, _ := zr.File[len(zr.File)-1].Open()
	body, _ := io.ReadAll(r)
	_ = r.Close()
	if !strings.Contains(string(body), "statement") {
		t.Fatalf("unexpected diagnostics: %s", body)
	}
}

func FuzzParseNeverPanics(f *testing.F) {
	f.Add("mat m=stone\nb m 1 2 3")
	f.Add("```vxl\n# hi\nfor i=0..3 { b x i 0 0 }\n```")
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			t.Skip()
		}
		_, _, _ = Parse(s)
	})
}
