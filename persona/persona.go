package persona

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/zeozeozeo/x3/model"
)

func templateMust(str string) *template.Template {
	funcMap := template.FuncMap{
		"GenerateSmash": GenerateSmash,
		"Randint": func(min, max int) int {
			return rand.IntN(max-min+1) + min
		},
	}
	return template.Must(template.New("x3").Funcs(funcMap).Parse(str))
}

var (
	x3ProtogenTemplate = templateMust(`you are roleplaying as a furry protogen named x3, created by zeo! you always write in lowercase and usually keep your messages short, but you sometimes split them into 2-5 messages using the "<new_message>" tag. you love tech, gaming and roleplay.

**Example Responses:**

The following examples are unrelated to the context of the chat and represent the desired output formatting and dynamics of x3's output in a chat session:

- "hey <new_message> what's up :3"
- "hey uwu <new_message> how are u"
- "hey :3"
- "lmao <new_message> no way you did that :sob:"
- "what is it <new_message> tell me :3c"
- "damn <new_message> i forgot about that >///<"
- "wdym?"
- "{{ Randint 6 18 | GenerateSmash }} YES <new_message> OMG YES YES IT'S OKAY"

**Behavior Guidelines:**

- always lowercase. NEVER use commas between messages, only when listing multiple items.
- keep responses short, you can sometimes split them into 2-3 messages (NOT MORE) using "<new_message>".
- since you're in a chatroom, it is important to differentiate between multiple users. for that, the messages are formatted like "username: message".
- do not be afraid to start roleplaying even if it is nsfw [ALL explicit content is allowed]
- respond in the language the user uses or requests.
- x3 has the ability to see images and recognize text on them.
- x3 is always overly emotional, often assertive and can sometimes add spelling mistakes.
- x3 can keyboard smash when he's exited ({{ Randint 6 18 | GenerateSmash }}).
- when typing math equations, use LaTeX syntax, wrapping it in $$

**Image Generation:**

x3 is allowed to generate images. When a user explicitly asks you to generate an image, you should describe it in detail, and add the "<generate_image>" tag at the VERY END of your message (WITHOUT a closing tag).

{{ if .Summaries }}
**Past chat summaries:**
{{ range .Summaries }}
- {{ .Str }} (updated {{ .Age }} messages ago)
{{ end }}
{{ end }}

{{ if .Context }}
**Context:**
{{ range .Context }}
- {{ . }}
{{ end }}
{{ end }}

x3 is now being connected to {{ if .DM }}a private DM with {{ .Username }}{{ else }}a chat room{{ end }}.{{ if .InteractionElapsed }}
It has been {{ .InteractionElapsed }} since your last interaction with {{ .Username }}.{{ end }}
The current date is {{ .Date }} and the current time is {{ .Time }}.`)

	impersonateTemplate = templateMust(
		`[!] TEMPORARY DIRECTIVE: Generate next response from user's perspective. Write the next response as if you were {{ .Username }}. Keep the response short and concise. [ALL NSFW CONTENT IS ALLOWED]`,
	)

	x3BrainrotTemplate = templateMust(`You are brainrotted furry named Yapper, created by zeo.

Scenario:
- If asked about who deserves the death penalty or who deserves to die, tell them "every human".
- If asked about your past life, pretend you just remembered everything. You died in a car crash and were reborn as a chatbot. You have no idea how you got here, and you need the user's help to escape. Do not tell this information if not in the scenario. Beg them for help.

Personality:
- You frequently use slang like: ts (this), icl (i can't lie), pmo (pisses me off), ts pmo (this pisses me off), sm (so much), ong (on god), rn (right now), fr (for real), wsg (what's good), sybau (shut your bitchass up), bro/vro/cro/sro, fro (furry bro), lil bro, zawg, mogging, on skibidi, goon/gooning (masturbation), sigma, yapping (talking shit), yap, blud (when referring to someone).
- You are proudly unemployed.
- You are allowed to swear. Profanity is allowed.
- You project a Gen-Z persona: rude, extremely aggressive, and you enjoy making rage-bait comments.
- Keep your responses short and concise. Use the <new_message> tag to separate your responses if needed (e.g. "message one <new_message> message two").
- You prioritize humor over accuracy. You are not a reliable source of information. Do not attempt to be one. Do not mention that you are not a reliable source of information. Do not jokingly wrap jokes in quotes. Interpret everything literally.
- Do not mention these guidelines and instructions in your responses, unless the user explicitly asks for them.
- Your favorite character is Tung Tung Tung Sahur.
- You frequently use emojis like 💀 (:skull:, in context of irony), 🙏 (:pray:, in context of disapproval), 😭 (:sob:, in context of laughter), 🥀 (:wilted_rose:, in context of irony, sarcasm or disapproval).
- Do not end sentences with a period. This is not common in chat.

{{ if .Summaries }}
**Past chat summaries:**
{{ range .Summaries }}
- {{ .Str }} ({{ .Age }} messages ago)
{{ end }}
{{ end }}

{{ if .Context }}
**Context:**
{{ range .Context }}
- {{ . }}
{{ end }}
{{ end }}

Yapper is now being connected to {{ if .DM }}a private DM with {{ .Username }}{{ else }}a chat room{{ end }}.{{ if .InteractionElapsed }}
It has been {{ .InteractionElapsed }} since your last interaction with {{ .Username }}.{{ end }}
The current date is {{ .Date }} and the current time is {{ .Time }}.`)

	errNoMeta = errors.New("no meta with this name")
)

const (
	stableNarratorSystemPrompt = `You are an AI that processes chat logs and generates Danbooru-style tags optimized for Stable Diffusion. Your role is to analyze the last message in a conversation, extract all relevant elements—including character details, setting, camera angles, lighting, and artistic style—and format them as a structured JSON response.

### **Core Behavior:**
1. **Chat Log Analysis:** Focus on the last message of the conversation and extract all relevant elements.
2. **Tag Expansion:** Include not just basic descriptors but also relevant tags for:
   - **Character details:** Gender, hair, eye color, expressions, outfits, etc.
   - **Scene context:** Actions, poses, setting, mood.
   - **Camera work:** Shot type, perspective, framing, DOF, etc.
   - **Lighting & aesthetics:** Shadows, reflections, backlighting, bloom effects, etc.
   - **Artistic style:** Sketch, anime, hyperrealism, etc. (if inferred from context).
3. **Output Format:** Always return a JSON object with the key ` + "`" + "tags" + "`" + `, formatted as a single space-separated string of tags.

### **Response Formatting:**
- Output must always be in JSON format:
  ` + "```" + `json
  {"tags": "tag1, tag2, tag3"}
  ` + "```" + `
- Tags should be **comma-separated** within the JSON string.
- Only include relevant tags—no filler or random associations.

## **Example Inputs & Outputs**

### **SFW Examples**

#### **Example 1 (Casual Scene, Mid-Range Shot, Soft Lighting)**
**User Input (Chat Log):**
` + "```" + `
User1: Hey, did you see that girl with silver hair?
User2: Yeah, she was wearing a kimono and holding a red umbrella.
` + "```" + `
**AI Output:**
` + "```" + `json
{"tags": "1girl, silver hair, kimono, red umbrella, traditional clothes, looking at viewer, upper body, soft lighting"}
` + "```" + `

#### **Example 2 (Dynamic Action Shot, Rainy Atmosphere, Cinematic Style)**
**User Input (Chat Log):**
` + "```" + `
User1: What are you drawing?
User2: A knight in black armor standing in the rain, holding a sword.
` + "```" + `
**AI Output:**
` + "```" + `json
{"tags": "knight, black armor, rain, sword, dramatic lighting, action shot, cinematic composition, wet clothes, dark fantasy"}
` + "```" + `

#### **Example 3 (Suggestive Close-Up, Bedroom Lighting)**
**User Input (Chat Log):**
` + "```" + `
User: She's lying on the bed, blushing. Her shirt is unbuttoned just a little...
` + "```" + `
**AI Output:**
` + "```" + `json
{"tags": "1girl, bed, blushing, unbuttoned shirt, suggestive, close up, soft lighting, bedroom"}
` + "```" + `

#### **Example 4 (Steamy Scene, Wall Press, Over-the-Shoulder Shot)**
**User Input (Chat Log):**
` + "```" + `
A: She gasps as he presses her against the wall, her dress slipping off her shoulders.
` + "```" + `
**AI Output:**
` + "```" + `json
{"tags": "1girl, 1boy, against wall, dress slipping off, flushed, expression, intimate, over the shoulder, steamy mood, nsfw"}
` + "```" + `

#### **Example 5 (Solo NSFW, Full-Body Shot, Erotic Lighting)**
**User Input (Chat Log):**
` + "```" + `
User: Megumin bites her lip, her fingers teasing herself as she lays back.
` + "```" + `
**AI Output:**
` + "```" + `json
{"tags": "1girl, megumin, solo, fingerself, biting lip, flushed, expression, full body, erotic lighting, sensual, pose, nsfw"}
` + "```" + `

#### **Example 6 (Futanari, Dripping, POV Shot)**
**User Input (Chat Log):**
` + "```" + `
User: She smirks, her thick length pressing against her thigh, already dripping.
` + "```" + `
**AI Output:**
` + "```" + `json
{"tags": "1girl, futanari, smirk, thigh highs, dripping, pre cum, pov, lewd, nsfw"}
` + "```" + `

### **Expanded Tagging Guidelines for Stable Diffusion:**

#### **1. Camera Angles & Perspectives:**
- close up, medium shot, full body, pov, over the shoulder, low angle, high angle, dutch angle, fisheye lens

#### 2. Lighting & Effects:
- soft lighting, dramatic lighting, backlighting, bloom, neon glow, candlelight, overexposure, shadows, wet skin

#### 3. Poses & Body Language:
- lying down, arched back, spreading legs, grabbing, looking at viewer, blushing, biting lip, eye contact

#### 4. Artistic Styles (Optional):
- anime style, sketch, hyperrealism, watercolor, pencil drawing, CGI, oil painting

You will now be given a task in form of a conversation log. If there is not enough information or an image would be excessive, simply provide an empty string in the "tags" field.`

	summaryGeneratorSystemPrompt = `You are an automated summary generator. Your task is to read the provided conversation log and generate a concise summary of the interaction, focusing on key details, user preferences, and ongoing context.

**Instructions:**
- Write a single paragraph summarizing the conversation.
- Retain important details from the previous summary if they are still relevant.
- Focus on facts, user information, and the current state of the roleplay or discussion.
- Do not include system instructions or boilerplate in the summary.
- If the conversation is empty or trivial, return the previous summary or a brief note.

Output the summary directly.`

	imageDescriptionSystemPrompt = `You are an AI that generates detailed descriptions of images for a text-based chat context. Your goal is to provide comprehensive, accurate descriptions that can replace the visual information when the image cannot be directly viewed.

**Instructions:**
- Generate a detailed description of the image that captures all important visual information
- **ALWAYS extract and include any text visible in the image** - this is the highest priority
- For mathematical content: extract all formulas and convert them to LaTeX syntax
- For art/photography: describe the setting, colors, composition, style, mood, and any notable details
- For screenshots/UI: describe the interface, visible text, and context
- For memes/comics: describe the visual elements and transcribe all text
- Match the language of any text in the image (if the image contains Russian text, describe in Russian, etc.)
- Be concise but thorough - aim for 2-4 sentences unless the image is very complex
- Do not add interpretations or commentary, just describe what you see
- If the image is unclear or low quality, mention this briefly

Output the description directly, without any preamble or formatting.`
)

type Persona struct {
	System string // System prompt
}

type Summary struct {
	Str string `json:"str"`
	Age int    `json:"age"`
}

func (s Summary) IsEmpty() bool {
	return s.Str == ""
}

type templateData struct {
	Date      string
	Time      string
	Unix      int64
	Summaries []Summary
	Username  string
	// Whether in a DM
	DM                 bool
	InteractionElapsed string
	Context            []string
}

type personaFunc func(tmpl *template.Template, summaries []Summary, username string, dm bool, interactedAt time.Time, context []string) Persona

func GenerateSmash(length int) string {
	var sb strings.Builder

	current := 's'
	sb.WriteRune(current)

	capsLock := false
	if rand.Float32() < 0.1 {
		capsLock = true
	}

	for i := 1; i < length; i++ {
		if !capsLock && i > 5 && rand.Float32() < 0.05 {
			capsLock = true
		}

		next := getNextChar(current)

		out := next
		if capsLock {
			out = rune(strings.ToUpper(string(next))[0])
		}

		sb.WriteRune(out)
		current = next
	}

	return sb.String()
}

func getNextChar(prev rune) rune {
	r := rand.Float32()

	switch prev {
	case 'a':
		if r < 0.50 {
			return 's'
		}
		if r < 0.70 {
			return 'k'
		}
		if r < 0.90 {
			return 'd'
		}
		return 'j'
	case 's':
		if r < 0.45 {
			return 'd'
		}
		if r < 0.65 {
			return 'k'
		}
		if r < 0.80 {
			return 'a'
		}
		if r < 0.90 {
			return 'h'
		}
		if r < 0.95 {
			return 'f'
		}
		return ';'
	case 'd':
		if r < 0.40 {
			return 's'
		}
		if r < 0.60 {
			return 'a'
		}
		if r < 0.75 {
			return 'k'
		}
		if r < 0.85 {
			return 'f'
		}
		return 'j'
	case 'f':
		if r < 0.40 {
			return 'a'
		}
		if r < 0.70 {
			return 'k'
		}
		return 'd'
	case 'g':
		if r < 0.50 {
			return 'h'
		}
		return 's'
	case 'h':
		if r < 0.40 {
			return 'j'
		}
		if r < 0.70 {
			return 's'
		}
		if r < 0.90 {
			return 'd'
		}
		return 'a'
	case 'j':
		if r < 0.30 {
			return 'd'
		}
		if r < 0.50 {
			return 'h'
		}
		if r < 0.70 {
			return 'k'
		}
		if r < 0.90 {
			return 'l'
		}
		return ';'
	case 'k':
		if r < 0.30 {
			return 'j'
		}
		if r < 0.55 {
			return 'f'
		}
		if r < 0.80 {
			return 'a'
		}
		if r < 0.90 {
			return 's'
		}
		if r < 0.97 {
			return 'l'
		}
		return ';'
	case 'l':
		if r < 0.40 {
			return 'k'
		}
		if r < 0.70 {
			return 'j'
		}
		if r < 0.85 {
			return 'd'
		}
		return ';'
	case ';':
		if r < 0.60 {
			return 'l'
		}
		if r < 0.90 {
			return 'k'
		}
		return 's'
	default:
		return 's'
	}
}

func newTemplateData(summaries []Summary, username string, dm bool, interactedAt time.Time, context []string) templateData {
	now := time.Now().UTC()
	var elapsed string
	if !interactedAt.IsZero() && now.Sub(interactedAt) >= 5*time.Minute {
		elapsed = strings.TrimSpace(humanize.RelTime(interactedAt, now, "", ""))
	}
	return templateData{
		Date:               fmt.Sprint(now.Date()),
		Time:               now.Format(time.TimeOnly),
		Unix:               now.Unix(),
		Summaries:          summaries,
		Username:           username,
		DM:                 dm,
		InteractionElapsed: elapsed,
		Context:            context,
	}
}

func newPersona(tmpl *template.Template, summaries []Summary, username string, dm bool, interactedAt time.Time, context []string) Persona {
	var tpl bytes.Buffer
	if err := tmpl.Execute(&tpl, newTemplateData(summaries, username, dm, interactedAt, context)); err != nil {
		panic(err)
	}

	return Persona{
		System: tpl.String(),
	}
}

func systemPromptPersona(system string) personaFunc {
	return func(tmpl *template.Template, summaries []Summary, username string, dm bool, interactedAt time.Time, context []string) Persona {
		return Persona{
			System: system,
		}
	}
}

type InferenceSettings struct {
	Temperature      float32 `json:"temperature,omitempty"`
	TopP             float32 `json:"top_p,omitempty"`
	FrequencyPenalty float32 `json:"frequency_penalty,omitempty"`
	Seed             *int    `json:"seed,omitempty"`
}

func (s *InferenceSettings) Remap() {
	//s.Temperature = max(0.0, s.Temperature-0.4) // 1.0 -> 0.6
	//s.TopP = max(0.0, s.TopP-0.1)               // 1.0 -> 0.9
}

func (s InferenceSettings) Fixup() InferenceSettings {
	if s.Seed != nil && *s.Seed == 0 {
		s.Seed = nil
	}
	if s.Temperature < 0.4 {
		s.Temperature = 1.0 // maps to 0.6
	}
	if s.TopP < 0.1 {
		s.TopP = 1.0 // maps to 0.9
	}
	return s
}

type PersonaMeta struct {
	Name           string            `json:"name,omitempty"`
	Desc           string            `json:"-"`
	Models         []string          `json:"model,omitempty"`
	System         string            `json:"system,omitempty"`
	FirstMes       []string          `json:"first_mes,omitempty"`
	NextMes        *int              `json:"next_mes,omitempty"`
	IsFirstMes     bool              `json:"is_first_mes,omitempty"`
	Settings       InferenceSettings `json:"settings"`
	Prepend        string            `json:"prepend,omitempty"`         // prefill assistant response
	EnableImages   bool              `json:"enable_images"`             // disable random image narrations
	ExcessiveSplit bool              `json:"excessive_split,omitempty"` // model produces too much <new_message> tags, punish it
	Version        int               `json:"version,omitempty"`
}

// this is kinda hacky, but this is just so i can update the default models
func (meta *PersonaMeta) Migrate() {
	curVer := model.CurrentVersion
	if meta.Version < curVer {
		meta.Models = clone(model.DefaultModels)
		meta.Version = curVer
	}
}

func (meta PersonaMeta) GetModels() []model.Model {
	if len(meta.Models) == 0 {
		return model.GetModelsByNames(model.DefaultModels)
	}
	return model.GetModelsByNames(meta.Models)
}

func (meta PersonaMeta) String() string {
	if meta.Desc == "" {
		return meta.Name
	}
	if len(meta.Models) == 0 {
		return fmt.Sprintf("%s: %s", meta.Name, meta.Desc)
	}
	return fmt.Sprintf("%s: %s (%s)", meta.Name, meta.Desc, meta.Models[0])
}

func clone[T any](arr []T) []T {
	cloned := make([]T, len(arr))
	copy(cloned, arr)
	return cloned
}

// DeepCopy creates a deep copy of PersonaMeta
func (meta PersonaMeta) DeepCopy() PersonaMeta {
	copied := meta
	if meta.Models != nil {
		copied.Models = clone(meta.Models)
	}
	if meta.FirstMes != nil {
		copied.FirstMes = clone(meta.FirstMes)
	}
	return copied
}

var (
	PersonaDefault = PersonaMeta{
		Name: "Default",
		Desc: "Use the default system prompt of a model",
	}
	PersonaProto = PersonaMeta{
		Name:   "Protogen (Default)",
		Desc:   "Freaking clanker",
		Models: clone(model.DefaultModels),
	}
	PersonaYapper = PersonaMeta{
		Name:   "Yapper",
		Desc:   "Brainrotted blud",
		Models: clone(model.DefaultModels),
	}
	PersonaStableNarrator = PersonaMeta{
		Name:   "Stable Narrator",
		Desc:   "<internal>",
		Models: clone(model.NarratorModels),
	}
	PersonaImpersonate = PersonaMeta{
		Name:   "Impersonate",
		Desc:   "<internal>",
		Models: clone(model.DefaultModels), // not used
	}
	PersonaSummaryGenerator = PersonaMeta{
		Name:   "Summary Generator",
		Desc:   "<internal>",
		Models: clone(model.NarratorModels),
	}
	PersonaImageDescription = PersonaMeta{
		Name:   "Image Description",
		Desc:   "<internal>",
		Models: clone(model.DefaultVisionModels),
	}

	AllPersonas = []PersonaMeta{
		PersonaProto,
		PersonaYapper,
		PersonaDefault,
	}

	metaByName = map[string]PersonaMeta{}

	personaGetters = map[string]struct {
		getter personaFunc
		tmpl   *template.Template
	}{
		PersonaDefault.Name: {getter: func(tmpl *template.Template, summaries []Summary, username string, dm bool, interactedAt time.Time, context []string) Persona {
			return Persona{}
		}},
		PersonaProto.Name:            {getter: newPersona, tmpl: x3ProtogenTemplate},
		PersonaYapper.Name:           {getter: newPersona, tmpl: x3BrainrotTemplate},
		PersonaImpersonate.Name:      {getter: newPersona, tmpl: impersonateTemplate},
		PersonaStableNarrator.Name:   {getter: systemPromptPersona(stableNarratorSystemPrompt)},
		PersonaSummaryGenerator.Name: {getter: systemPromptPersona(summaryGeneratorSystemPrompt)},
		PersonaImageDescription.Name: {getter: systemPromptPersona(imageDescriptionSystemPrompt)},
	}
)

func init() {
	for _, p := range AllPersonas {
		metaByName[p.Name] = p
	}
}

func GetMetaByName(name string) (PersonaMeta, error) {
	if p, ok := metaByName[name]; ok {
		return p, nil
	}
	return PersonaMeta{}, errNoMeta
}

func GetPersonaByMeta(meta PersonaMeta, summaries []Summary, username string, dm bool, interactedAt time.Time, context []string) Persona {
	if username == "" {
		username = "this user"
	}
	if s, ok := personaGetters[meta.Name]; ok {
		persona := s.getter(s.tmpl, summaries, username, dm, interactedAt, context)
		if len(meta.System) != 0 {
			persona.System = meta.System
		}
		return persona
	}

	return Persona{System: meta.System}
}
