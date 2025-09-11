package commands

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"image"
	"image/gif"
	_ "image/jpeg"
	"image/png"

	"golang.org/x/image/draw"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	_ "embed"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/zeozeozeo/x3/media"
)

var bubbleImage image.Image

func init() {
	var err error
	bubbleImage, err = png.Decode(bytes.NewReader(media.SpeechBubblePng))
	if err != nil {
		panic(err)
	}
}

func httpGetSlurp(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func scaleImage(src image.Image, width, height int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

func drawImageOverlay(base image.Image, overlay image.Image) image.Image {
	baseBounds := base.Bounds()
	bW := baseBounds.Dx()
	bH := baseBounds.Dy()

	maxOverlayHeight := int(float64(bH) * 0.2) // 20% the height of base image
	overlayAspect := float64(overlay.Bounds().Dy()) / float64(max(overlay.Bounds().Dx(), 1))

	oW := bW
	oH := max(min(int(float64(bW)*overlayAspect), maxOverlayHeight), 1)

	scaledOverlay := scaleImage(overlay, oW, oH)
	out := image.NewRGBA(image.Rect(0, 0, bW, bH))
	draw.Draw(out, out.Bounds(), base, baseBounds.Min, draw.Src)
	draw.Draw(out, image.Rect(0, 0, oW, oH), scaledOverlay, scaledOverlay.Bounds().Min, draw.Over)

	return out
}

func processStatic(data []byte, overlay image.Image) ([]byte, error) {
	slog.Info("processStatic: decode")
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	slog.Info("processStatic: overlay")
	result := drawImageOverlay(img, overlay)

	slog.Info("processStatic: encode")
	buf := &bytes.Buffer{}
	err = png.Encode(buf, result)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func processGIF(data []byte, overlay image.Image) ([]byte, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	outGIF := &gif.GIF{
		LoopCount: g.LoopCount,
		Delay:     make([]int, len(g.Image)),
		Disposal:  make([]byte, len(g.Image)),
	}

	for i, frame := range g.Image {
		bounds := frame.Bounds()

		// paletted -> RGBA
		rgbaFrame := image.NewRGBA(bounds)
		draw.Draw(rgbaFrame, bounds, frame, bounds.Min, draw.Src)

		// draw overlay
		overlaid := drawImageOverlay(rgbaFrame, overlay)

		// RGBA -> paletted
		palFrame := image.NewPaletted(overlaid.Bounds(), g.Image[i].Palette)
		draw.Draw(palFrame, overlaid.Bounds(), overlaid, image.Point{}, draw.Src)

		outGIF.Image = append(outGIF.Image, palFrame)
		outGIF.Delay[i] = g.Delay[i]
		outGIF.Disposal[i] = g.Disposal[i]
	}

	buf := &bytes.Buffer{}
	err = gif.EncodeAll(buf, outGIF)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func HandleSay(event *events.MessageCreate) error {
	messages, err := fetchMessagesBefore(event.Client(), event.ChannelID, event.MessageID, 100)
	messages = append([]discord.Message{event.Message}, messages...)
	if err != nil || len(messages) == 0 {
		return sendPrettyError(event.Client(), "Couldn't fetch message history :(\n"+err.Error(), event.ChannelID, event.MessageID)
	}

	var data []byte
	var isGif, isSpoiler bool

	// iterate newest to oldest
outer:
	for _, msg := range messages {
		for _, attachment := range msg.Attachments {
			if !isImageAttachment(attachment) {
				continue
			}
			isGif = *attachment.ContentType == "image/gif"
			isSpoiler = strings.HasPrefix(attachment.Filename, "SPOILER_")

			slog.Info("HandleSay: fetching image")
			data, err = httpGetSlurp(attachment.URL)
			if err != nil {
				return sendPrettyError(event.Client(), err.Error(), event.ChannelID, event.MessageID)
			}
			break outer
		}
	}
	if len(data) == 0 {
		return nil
	}

	slog.Info("HandleSay: process image", "isGif", isGif)
	var outData []byte
	if isGif {
		outData, err = processGIF(data, bubbleImage)
	} else {
		outData, err = processStatic(data, bubbleImage)
	}
	if err != nil {
		return sendPrettyError(event.Client(), err.Error(), event.ChannelID, event.MessageID)
	}

	filename := "say.gif"
	if isSpoiler {
		filename = "SPOILER_" + filename
	}

	slog.Info("HandleSay: send response", "len", len(outData))
	_, err = event.Client().Rest().CreateMessage(
		event.ChannelID,
		discord.NewMessageCreateBuilder().
			SetMessageReferenceByID(event.MessageID).
			SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
			AddFile(filename, "image/gif", bytes.NewReader(outData)).
			Build(),
	)

	return err
}
