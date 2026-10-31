package openaiapi

import (
	"log/slog"
	"sort"

	"github.com/zeozeozeo/x3/model"
)

// providerOrder returns the providers to try for a model.
//
// It starts with x3's router, which only knows the providers listed in the
// `providers_order` of models.json. Providers that are configured for the
// model but missing from that list are appended, so a model stays usable even
// when its provider was never added to the trial order.
func providerOrder(m model.Model) []*model.ScoredProvider {
	ordered := model.ProvidersForModel(m, m.Reasoning)

	seen := make(map[string]bool, len(ordered))
	for _, provider := range ordered {
		seen[provider.Name] = true
	}

	missing := make([]string, 0)
	for name := range m.Providers {
		if !m.ProviderEnabled(name) {
			continue
		}
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)

	for _, name := range missing {
		slog.Info("provider is not part of providers_order, appending it",
			"model", m.Command, "provider", name)
		ordered = append(ordered, &model.ScoredProvider{Name: name})
	}

	return ordered
}
