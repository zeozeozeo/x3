# VXL language reference

VXL is the small, deterministic voxel drawing language used by `/schematic`. It is designed to be compact enough for an LLM to generate while remaining safe to execute: VXL is interpreted by the bot and has no filesystem, network, process, reflection, or host-language access.

VXL programs define materials, calculate integer coordinates, and place voxel primitives inside a bounded Minecraft schematic canvas. The Y axis is vertical. Coordinates start at zero and all primitive endpoints are inclusive.

## Quick example

```vxl
mat stone = stone_bricks
mat timber = stripped_oak_log
mat glass = light_blue_stained_glass
mat empty = air

# A hollow building shell and a separate floor
hbox stone 8 1 8 39 16 31 1
box timber 9 1 9 38 1 30

# Reusable framed window. The first box carves through the wall.
template window(x y z w h) {
  at x y z {
    box empty 0 0 0 w h 2
    hbox timber 0 0 0 w h 2 1
    box glass 1 1 1 (w-1) (h-1) 1
  }
}

paste window 12 6 7 5 5
paste window 29 6 7 5 5

# A doorway must also be carved through every wall layer.
box empty 21 1 7 26 9 9
hbox timber 21 1 7 26 9 9 1

# Two repeated diagonal surfaces form a pitched roof with an overhang.
mat roof = dark_oak_planks
for z = 6..33 {
  ln roof 6 16 z 24 25 z 1
  ln roof 24 25 z 41 16 z 1
}
```

## Lexical rules

- VXL is line-oriented. Put each statement on its own line. A semicolon can also terminate a statement.
- Spaces, tabs, and commas separate tokens. Commas are optional.
- `#` begins a comment, except when followed by exactly six hexadecimal digits as part of a color such as `#4FAE62`.
- Keywords and primitive names are case-insensitive. Keep user-defined material, variable, parameter, and template names consistently cased.
- Integers are the only value type. There are no strings, floating-point values, arrays, objects, or null values.
- A program may be returned raw or inside a `vxl` code fence. A fenced program is safest because text after the closing fence is ignored.

```vxl
# These statements are equivalent.
b stone 1 2 3
b stone, 1, 2, 3;
```

## Materials

Declare a material alias before using it:

```vxl
mat wall = stone_bricks
mat leaves = minecraft:oak_leaves
mat accent = #D04A35
mat empty = air
```

The canonical keyword for materials is `mat`. For leniency with generated programs, an unmistakable RGB assignment such as `let accent = #D04A35` is also accepted as a material declaration. `let` followed by an integer expression remains a normal variable declaration.

A material value may be:

- A Minecraft Java block ID, with or without the `minecraft:` namespace.
- A six-digit RGB color. It is mapped to a visually close block while preferring relatively inexpensive survival materials.
- `air`, which removes blocks written earlier.

Block states and NBT properties are not part of VXL. Materials resolve against the pinned block catalog. For legacy MCEdit output, modern blocks are converted to an appropriate legacy fallback.

Writes are ordered. A later primitive replaces earlier blocks at overlapping coordinates. Use this deliberately for surface layers and carving:

```vxl
box wall 10 1 10 30 12 25
box empty 18 1 9 22 7 12   # actually cuts through the wall
box door 19 1 10 21 6 10  # adds a door after carving
```

## Expressions and variables

`let` binds or replaces an integer variable:

```vxl
let center = 32
let radius = 8
sph stone center 16 center radius
```

Expressions support:

| Precedence, high to low | Operators | Meaning |
| --- | --- | --- |
| Unary | `+x`, `-x`, `!x` | Identity, negation, logical not |
| Multiplicative | `*`, `/`, `%` | Multiply, integer divide, remainder |
| Additive | `+`, `-` | Add and subtract |
| Comparison | `==`, `!=`, `<`, `<=`, `>`, `>=` | Produce `1` or `0` |
| Logical AND | `&&` | Short-circuit AND |
| Logical OR | `\|\|` | Short-circuit OR |

Parentheses may be nested. Division and remainder by zero are errors. Integer overflow is rejected. In conditions, zero is false and every nonzero value is true.

## Control flow

### Loops

Loop ranges are inclusive:

```vxl
for x = 2..10 {
  b stone x 4 8
}

for y = 20..2 step -2 {
  sph leaves 16 y 16 2
}
```

If `step` is omitted, it is `1` for an ascending range and `-1` for a descending range. A zero step or a step moving away from the endpoint is rejected. The loop variable is restored after the loop.

### Conditions

`else` is optional:

```vxl
for x = 0..15 {
  for z = 0..15 {
    if ((x+z)%2 == 0) {
      b light x 0 z
    } else {
      b dark x 0 z
    }
  }
}
```

Conditions only evaluate VXL integer expressions. They do not execute JavaScript or call host functions.

### Translation

`at` adds an offset to every voxel written by its body. Translations nest and are useful for assembling components in local coordinates:

```vxl
at 20 5 30 {
  box stone 0 0 0 6 3 6
  at 3 4 3 {
    sph glow 0 0 0 2
  }
}
```

The offset affects drawing coordinates, not ordinary expression values.

## Templates and `paste`

Templates are reusable groups of VXL statements with integer parameters:

```vxl
template pillar(x z height) {
  box stone x 0 z x height z
  sph cap x (height+1) z 2
}

paste pillar 5 8 12
paste pillar 24 8 (10+4)
```

Template rules:

- Declare a template before it is pasted.
- Parameters are names inside parentheses, separated by spaces or optional commas.
- `paste` is followed by the template name and exactly one integer expression per parameter.
- Paste arguments are evaluated in the caller before parameters are bound.
- Parameters and variables created with `let` inside the template are scoped to that paste. They cannot overwrite the caller's variables.
- Materials and previously declared templates remain available inside a template.
- A template has no return value and cannot invoke host-language code.
- Recursive template pastes are stopped by the nesting limit; total expansion also has a fixed limit.

Templates are particularly useful for windows, columns, trees, wheels, roof ribs, façade modules, and other detailed repeated components.

## Drawing primitives

All coordinates and endpoints are inclusive. Optional values are shown in brackets; do not type the brackets.

### Single block

```text
b material x y z
block material x y z
```

Places one block. `b` and `block` are aliases.

### Line and thick line

```text
ln material x1 y1 z1 x2 y2 z2 [radius]
line material x1 y1 z1 x2 y2 z2 [radius]
```

Draws a line between two endpoints. With a positive radius, spherical cross-sections create a rounded thick line.

### Filled and hollow boxes

```text
box  material x1 y1 z1 x2 y2 z2
hbox material x1 y1 z1 x2 y2 z2 [thickness]
```

`box` fills the entire rectangular volume. `hbox` writes only its shell; thickness defaults to `1`. A thickness large enough to consume the interior produces a solid result.

For buildings, rooms, cabins, and containers, normally begin with `hbox`. A large `box` creates a solid mass with no usable interior.

### Filled and hollow spheres

```text
sph    material cx cy cz radius
sphere material cx cy cz radius
hsph   material cx cy cz radius [thickness]
```

`sph` and `sphere` are aliases for a filled sphere. `hsph` creates a shell with thickness defaulting to `1`.

### Filled and hollow ellipsoids

```text
ell  material cx cy cz radiusX radiusY radiusZ
hell material cx cy cz radiusX radiusY radiusZ [thickness]
```

Ellipsoids are useful for organic bodies, domes, canopies, hulls, and other stretched round forms. Every radius must be positive.

### Cylinders and tubes

```text
cyl  material x1 y1 z1 x2 y2 z2 radius
tube material x1 y1 z1 x2 y2 z2 radius [thickness]
```

These may point in any 3D direction. `cyl` is solid. `tube` is hollow and defaults to thickness `1`.

## Hollow construction and carving

Visual material placed over a wall does not create an opening; it merely replaces the wall's outer blocks. Correct architecture generally follows this order:

1. Draw a hollow shell with `hbox`.
2. Add separate floors, supports, and partitions.
3. Carve doors and windows through the complete wall thickness with `air`.
4. Add frames, glass, doors, sills, and lintels after carving.
5. Add a roof with a readable profile, overhang, and thickness.

The same idea applies to vehicles, vessels, helmets, caves, domes, and any other structure that needs interior volume.

## Errors and bounds

Parsing and execution errors report a line, column, message, source line, and caret where possible. Typical errors include:

- Unknown statements, variables, materials, or templates.
- Wrong template argument counts.
- Missing braces or parentheses.
- Coordinates outside the requested canvas.
- Invalid radii or shell thicknesses.
- Division by zero or integer overflow.
- Resource-limit violations.

The schematic generator sends these diagnostics back to the model for up to five correction attempts.

## Safety and resource limits

VXL is not JavaScript and is never passed to a shell or general-purpose evaluator. The interpreter exposes only the statements documented here. Current limits are:

| Resource | Limit |
| --- | ---: |
| Canvas axis | 320 blocks |
| Occupied blocks | 4,000,000 |
| Block-write attempts | 16,000,000 |
| Primitive calls | 250,000 |
| Loop iterations | 1,000,000 |
| Template pastes | 100,000 |
| Source size | 128 KiB |
| Parser/template nesting | 8 levels |
| Execution time | 5 seconds |

Every block write is bounds-checked. Context cancellation, execution timeout, nesting limits, loop limits, paste limits, primitive limits, and write limits prevent unbounded execution or memory growth.

## Practical guidance

- Use `hbox`, `hsph`, `hell`, and `tube` whenever a solid interior adds no value.
- Combine and overlap primitives so the finished silhouette does not look like a stack of basic shapes.
- Use loops for ribs, stripes, roof surfaces, gradients, repeated supports, and controlled texture.
- Use templates for detailed repeated motifs rather than duplicating many statements.
- Add depth around focal details: recess windows, project frames, taper limbs, layer roofs, and vary silhouette edges.
- Keep the subject readable from multiple directions, not only from the front.
- Use coherent material families, with restrained accents and structural variation across large surfaces.
- Remember that later writes win. Plan carving and decorative layers in the correct order.

## Compact grammar summary

```text
mat ALIAS = BLOCK_ID
mat ALIAS = #RRGGBB
let NAME = EXPR

for NAME = EXPR..EXPR [step EXPR] { STATEMENTS }
if EXPR { STATEMENTS } [else { STATEMENTS }]
at EXPR EXPR EXPR { STATEMENTS }

template NAME(PARAM...) { STATEMENTS }
paste NAME EXPR...

b|block MAT X Y Z
ln|line MAT X1 Y1 Z1 X2 Y2 Z2 [RADIUS]
box MAT X1 Y1 Z1 X2 Y2 Z2
hbox MAT X1 Y1 Z1 X2 Y2 Z2 [THICKNESS]
sph|sphere MAT CX CY CZ RADIUS
hsph MAT CX CY CZ RADIUS [THICKNESS]
ell MAT CX CY CZ RX RY RZ
hell MAT CX CY CZ RX RY RZ [THICKNESS]
cyl MAT X1 Y1 Z1 X2 Y2 Z2 RADIUS
tube MAT X1 Y1 Z1 X2 Y2 Z2 RADIUS [THICKNESS]
```
