package horder

import (
	"slices"

	"github.com/cloudflare/ahocorasick"
	"github.com/zeozeozeo/aihorde-go"
)

var nsfwMatcher = ahocorasick.NewStringMatcher([]string{
	"nsfw",
	"lewd",
	"pussy",
	"vagina",
	"naked",
	"nude",
	"nipples",
	"breasts",
	"penis",
	"fetish",
	"kinky",
	"pornograph", // -y -ic
	"see-through",
	"bdsm",
	"oral",
	"anal",
	"sex",
	"erotic",
	"intercourse",
	"masturbation",
	"foreplay",
	"orgy",
	"exhibitionism",
	"latex",
	"explicit",
	"undressing",
	"panties",
	"strapon",
})

func GetCensorship(gen aihorde.GenerationStable) (nsfw, csam bool) {
	nsfw = slices.ContainsFunc(gen.GenMetadata, func(g aihorde.GenerationMetadataStable) bool {
		return g.Type == aihorde.MetadataTypeInformation && g.Value == aihorde.MetadataValueNSFW
	})
	csam = slices.ContainsFunc(gen.GenMetadata, func(g aihorde.GenerationMetadataStable) bool {
		return g.Type == aihorde.MetadataTypeCensorship && g.Value == aihorde.MetadataValueCSAM
	})
	return
}

func IsPromptNSFW(prompt string) bool {
	return nsfwMatcher.Contains([]byte(prompt))
}
