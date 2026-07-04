package commands

import "regexp"

type detectedURL struct {
	raw         string
	angleQuoted bool
}

func detectURLs(content string, re *regexp.Regexp) []detectedURL {
	if re == nil || content == "" {
		return nil
	}

	indices := re.FindAllStringIndex(content, -1)
	if len(indices) == 0 {
		return nil
	}

	out := make([]detectedURL, 0, len(indices))
	for _, idx := range indices {
		start, end := idx[0], idx[1]
		angleQuoted := start > 0 && end < len(content) && content[start-1] == '<' && content[end] == '>'
		out = append(out, detectedURL{
			raw:         content[start:end],
			angleQuoted: angleQuoted,
		})
	}
	return out
}
