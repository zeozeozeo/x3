package horder

import (
	"slices"

	"github.com/zeozeozeo/aihorde-go"
)

func GetCensorship(gen aihorde.GenerationStable) (nsfw, csam bool) {
	nsfw = slices.ContainsFunc(gen.GenMetadata, func(g aihorde.GenerationMetadataStable) bool {
		return g.Type == aihorde.MetadataTypeInformation && g.Value == aihorde.MetadataValueNSFW
	})
	csam = slices.ContainsFunc(gen.GenMetadata, func(g aihorde.GenerationMetadataStable) bool {
		return g.Type == aihorde.MetadataTypeCensorship && g.Value == aihorde.MetadataValueCSAM
	})
	return
}
