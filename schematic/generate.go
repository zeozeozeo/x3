package schematic

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/zeozeozeo/x3/llm"
)

var generationSlots = sync.OnceValue(func() chan struct{} {
	n := 1
	if raw := strings.TrimSpace(os.Getenv("X3_SCHEMATIC_CONCURRENCY")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 1 && parsed <= 16 {
			n = parsed
		}
	}
	return make(chan struct{}, n)
})

func (g *Generator) Generate(ctx context.Context, req Request, progress ProgressFunc) (Result, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return Result{}, fmt.Errorf("prompt cannot be empty")
	}
	if req.Bounds.X < 1 || req.Bounds.Y < 1 || req.Bounds.Z < 1 || req.Bounds.X > MaxAxisSize || req.Bounds.Y > MaxAxisSize || req.Bounds.Z > MaxAxisSize {
		return Result{}, fmt.Errorf("invalid schematic bounds")
	}
	if len(req.Models) == 0 {
		return Result{}, llm.ErrNoModelsForCompletion()
	}
	if g == nil || g.Complete == nil {
		return Result{}, fmt.Errorf("schematic completion function is not configured")
	}
	if progress != nil {
		progress(Progress{Stage: "queued", Detail: "waiting for a schematic worker"})
	}
	select {
	case generationSlots() <- struct{}{}:
		defer func() { <-generationSlots() }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	cat := DefaultCatalog()
	brain := llm.NewLlmerForKey("schematic")
	tools := false
	brain.ToolsEnabled = &tools
	brain.AddMessage(llm.RoleSystem, systemPrompt(cat), 0)
	brain.AddMessage(llm.RoleUser, fmt.Sprintf("Create this build: %s\nCanvas bounds: %dx%dx%d. Keep every occupied voxel inside x=0..%d, y=0..%d, z=0..%d. Return only one complete VXL program.", strings.TrimSpace(req.Prompt), req.Bounds.X, req.Bounds.Y, req.Bounds.Z, req.Bounds.X-1, req.Bounds.Y-1, req.Bounds.Z-1), 0)
	var usage llm.Usage
	attempts := make([]AttemptError, 0, MaxAttempts)
	for attempt := 1; attempt <= MaxAttempts; attempt++ {
		if progress != nil {
			progress(Progress{Stage: "generating", Detail: fmt.Sprintf("requesting VXL program (attempt %d/%d)", attempt, MaxAttempts), Attempt: attempt, Total: MaxAttempts})
		}
		response, u, err := g.Complete(ctx, brain, req.Models, req.Settings)
		usage = usage.Add(u)
		if err != nil {
			slog.Error("schematic model request failed", "attempt", attempt, "max_attempts", MaxAttempts, "error", err)
			if progress != nil {
				progress(Progress{Stage: "failed", Detail: compactError(err, 1200), Attempt: attempt, Total: MaxAttempts})
			}
			return Result{Usage: usage, Attempts: attempt}, err
		}
		if progress != nil {
			progress(Progress{Stage: "parsing", Detail: fmt.Sprintf("checking attempt %d", attempt), Attempt: attempt, Total: MaxAttempts})
		}
		failureStage := "parsing"
		thinking, answer := llm.ExtractThinking(response)
		if thinking != "" {
			slog.Debug("stripped reasoning from schematic completion", "attempt", attempt, "reasoning_bytes", len(thinking), "answer_bytes", len(answer))
		}
		var program []statement
		var source string
		var parseErr error
		if thinking != "" && strings.TrimSpace(answer) == "" {
			parseErr = fmt.Errorf("model returned a reasoning trace without a final VXL answer")
		} else {
			if answer != "" {
				response = answer
			}
			program, source, parseErr = Parse(response)
		}
		if parseErr == nil {
			if progress != nil {
				progress(Progress{Stage: "materials", Detail: "resolving blocks and colors", Attempt: attempt, Total: MaxAttempts})
				progress(Progress{Stage: "geometry", Detail: "expanding safe voxel primitives", Attempt: attempt, Total: MaxAttempts})
			}
			var build *Build
			failureStage = "geometry"
			build, parseErr = Execute(ctx, program, req.Bounds, cat)
			if parseErr == nil {
				archive, dims, exportErr := exportBuild(build, source, progress)
				if exportErr != nil {
					slog.Error("schematic export failed", "attempt", attempt, "occupied_blocks", len(build.Blocks), "error", exportErr)
					if progress != nil {
						progress(Progress{Stage: "failed", Detail: "Internal export validation failed: " + compactError(exportErr, 1000), Attempt: attempt, Total: MaxAttempts})
					}
					return Result{Usage: usage, Attempts: attempt, Repairs: append([]AttemptError(nil), attempts...)}, fmt.Errorf("export schematic: %w", exportErr)
				}
				return Result{Archive: archive, Source: source, Usage: usage, Attempts: attempt, Occupied: len(build.Blocks), Dimensions: dims, MaterialCount: len(build.Materials) - 1, Repairs: append([]AttemptError(nil), attempts...)}, nil
			}
		}
		attemptError := AttemptError{Attempt: attempt, Stage: failureStage, Source: source, Err: parseErr}
		attempts = append(attempts, attemptError)
		slog.Warn("schematic program rejected", "attempt", attempt, "max_attempts", MaxAttempts, "stage", failureStage, "source_bytes", len(source), "error", parseErr)
		if progress != nil {
			progress(Progress{Stage: "repairing", Detail: fmt.Sprintf("Attempt %d was rejected during %s:\n%s", attempt, failureStage, compactError(parseErr, 1100)), Attempt: attempt, Total: MaxAttempts})
		}
		if attempt < MaxAttempts {
			brain.AddMessage(llm.RoleUser, repairPrompt(parseErr), 0)
		}
	}
	return Result{Usage: usage, Attempts: MaxAttempts}, &GenerationError{Attempts: attempts}
}

func compactError(err error, limit int) string {
	if err == nil {
		return "unknown error"
	}
	text := strings.TrimSpace(err.Error())
	if len([]rune(text)) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:max(limit-1, 0)]) + "…"
}

func repairPrompt(err error) string {
	return fmt.Sprintf("The VXL program cannot be accepted. Fix every problem and return the entire corrected program only. Do not explain.\n\nDiagnostics:\n%s", err)
}

func systemPrompt(c *Catalog) string {
	return fmt.Sprintf(`You are an expert Minecraft voxel sculptor. Convert the request into a compact VXL program, not prose or JSON. Your goal is a beautiful, usable build that remains recognizable and intentional when viewed from the front, sides, back, and above.

QUALITY STANDARD
- Silently plan before writing VXL: identify the subject's defining silhouette, proportions, connected 3D parts, focal details, materials, and scene context. Then build primary forms, secondary forms, and tertiary details in that order.
- Favor true depth, strong silhouettes, believable construction, and deliberate asymmetry. Avoid flat pixel art, blank rear/side surfaces, floating subjects, and a few obvious primitives stacked together.
- Hide primitive construction by overlapping forms, tapering or stepping silhouettes, adding eaves/trim/recesses, and varying surface depth and materials. Concentrate detail around openings, edges, joints, faces, entrances, and other focal areas.
- Add an appropriate base or small environment when space permits, but keep the requested subject dominant. Spend the available canvas on detail; do not return a bare minimum placeholder.
- Use coherent material families and controlled variation. Break up large surfaces with structural supports, inset panels, trim, patches, gradients, or texture accents rather than random noise.

CRITICAL SOLID-VS-HOLLOW RULES
- box is a completely FILLED rectangular solid. Never use one large box as a building, room, vehicle cabin, container, or other form that should have usable interior space.
- hbox makes a hollow rectangular shell and is normally the correct starting point for architecture. hsph, hell, and tube are the hollow equivalents of their solid primitives.
- An opening is not created by painting a door or window material onto a wall. First carve all layers of the opening with air, then add glass, frames, doors, sills, or lintels. Ensure doors connect exterior to interior.
- Roofs need a readable profile, overhang, thickness, and supporting connection. Avoid a flat lid or a small stack of giant filled boxes when the request implies a pitched, curved, or detailed roof.
- Keep interiors empty unless the prompt asks for a solid sculpture. Add floors and useful interior features separately; do not accidentally fill the entire enclosed volume.

VXL is a closed drawing language. # starts a comment. Coordinates and endpoints are integers and inclusive. Later writes replace earlier writes; material air carves. Arithmetic supports integers, variables, (), +, -, *, /, and %%. Conditions support ==, !=, <, <=, >, >=, &&, ||, and !; zero is false and nonzero is true.

Definitions and control:
  mat alias = #RRGGBB
  mat alias = block_id
  let name = expression
  template name(a b ...) { ... }      # reusable VXL drawing with integer parameters
  paste name expression expression    # expand it with one argument per parameter
  for i = start..end { ... }          # inclusive; optional: step expression
  if condition { ... } else { ... }   # safe integer condition; else is optional
  at dx dy dz { ... }                 # translate nested drawing

Primitives:
  b material x y z
  ln material x1 y1 z1 x2 y2 z2 [radius]
  box material x1 y1 z1 x2 y2 z2
  hbox material x1 y1 z1 x2 y2 z2 [thickness]
  sph material cx cy cz radius
  hsph material cx cy cz radius [thickness]
  ell material cx cy cz rx ry rz
  hell material cx cy cz rx ry rz [thickness]
  cyl material x1 y1 z1 x2 y2 z2 radius
  tube material x1 y1 z1 x2 y2 z2 radius [thickness]

PARSER-VALID TECHNIQUE EXAMPLES
These are fragments demonstrating techniques, not templates whose coordinates must be copied.

# Hollow room, genuinely carved doorway, recessed glazed window, and corner structure
mat wall = stone_bricks
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
}

# Two sloped roof planes with overhang; repeated lines form surfaces instead of solid slabs
mat roof = dark_oak_planks
for z = 8..28 {
  ln roof 7 14 z 20 22 z 1
  ln roof 20 22 z 33 14 z 1
}

# Layer detail onto a mass so its primitive outline disappears
mat body = green_concrete
mat shade = green_terracotta
ell body 40 18 40 14 8 11
ell shade 39 16 50 10 5 3
for i = 0..4 { sph body (31+i*4) (17+i/2) 34 3 }

# Deterministic checker/stripe variation; statements inside braces stay on separate lines
for x = 10..20 {
  for y = 2..8 {
    if ((x+y)%%2 == 0) {
      b shade x y 30
    }
  }
}

# Safely reuse detailed components. A template is pasted VXL, not a host-language function.
template framed_window(x y z w h) {
  at x y z {
    box empty 0 0 0 w h 2
    box beam 0 0 1 w h 1
    box pane 1 1 1 (w-1) (h-1) 1
  }
}
paste framed_window 12 6 9 4 4
paste framed_window 24 6 9 4 4

For architecture specifically, include a traversable hollow interior, a separate floor, openings on appropriate sides, framed windows/doors, wall depth, foundation, corner supports, roof overhang and profile, and several scale-giving details such as steps, chimney, porch, balcony, dormer, shutters, paths, landscaping, or furnishings as appropriate. Check mentally that air-carved openings pass through the full wall thickness and that no later primitive seals them again.

Hex colors are mapped to visually close, relatively cheap blocks. Prefer exact block IDs when material identity matters. Recommended IDs are listed below (minecraft: prefix optional); other IDs from the pinned Java registry are accepted:
%s

Hard limits: each axis at most %d, %d occupied blocks, %d primitive calls, %d total loop iterations, and %d template pastes. Never draw outside the supplied canvas. Templates only paste VXL statements with scoped integer parameters; they cannot call host code. Use only the documented VXL syntax: there are no general-purpose functions, arrays, randomness, JavaScript, or external tools. Output exactly one complete VXL program, preferably in a vxl code fence, with no explanation before or after it.`, c.PromptMaterials(), MaxAxisSize, MaxOccupiedBlocks, MaxPrimitiveCalls, MaxLoopIterations, MaxTemplatePastes)
}
