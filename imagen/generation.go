package imagen

import "fmt"

type GenerationRequest struct {
	Prompt   string `json:"prompt"`
	N        int    `json:"n"`
	Size     string `json:"size"` // "256x256", "512x512", "1024x1024"
	Model    string `json:"model"`
	Negative string `json:"negative_prompt"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type GenerationRequestBuilder struct {
	Prompt   string
	N        int
	Model    string
	Negative string
	Width    int
	Height   int
}

func NewGenerationRequestBuilder() *GenerationRequestBuilder {
	return &GenerationRequestBuilder{}
}

func (b *GenerationRequestBuilder) WithPrompt(prompt string) *GenerationRequestBuilder {
	b.Prompt = prompt
	return b
}

func (b *GenerationRequestBuilder) WithN(n int) *GenerationRequestBuilder {
	b.N = n
	return b
}
func (b *GenerationRequestBuilder) WithModel(model string) *GenerationRequestBuilder {
	b.Model = model
	return b
}

func (b *GenerationRequestBuilder) WithNegativePrompt(negative string) *GenerationRequestBuilder {
	b.Negative = negative
	return b
}

func (b *GenerationRequestBuilder) WithSize(width, height int) *GenerationRequestBuilder {
	b.Width = width
	b.Height = height
	return b
}

func (b *GenerationRequestBuilder) Build() *GenerationRequest {
	return &GenerationRequest{
		Prompt:   b.Prompt,
		N:        b.N,
		Size:     fmt.Sprintf("%dx%d", b.Width, b.Height),
		Model:    b.Model,
		Negative: b.Negative,
		Width:    b.Width,
		Height:   b.Height,
	}
}

func (g *GenerationRequest) Do() error {
	return nil
}
