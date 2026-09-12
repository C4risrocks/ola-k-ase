package main

import (
	"net/http"
	"regexp"
	"strings"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func readPostForm(r *http.Request) (Post, string) {
	title := strings.TrimSpace(r.FormValue("title"))
	slug := strings.TrimSpace(r.FormValue("slug"))
	summary := strings.TrimSpace(r.FormValue("summary"))
	content := strings.TrimSpace(r.FormValue("content"))
	tagsRaw := strings.TrimSpace(r.FormValue("tags"))

	if title == "" || summary == "" || content == "" {
		return Post{}, "Title, summary, and content are required."
	}
	if len(title) > maxPostTitleLen {
		return Post{}, "Title is too long."
	}
	if len(summary) > maxPostSummaryLen {
		return Post{}, "Summary is too long."
	}
	if len(content) > maxPostContentLen {
		return Post{}, "Content is too long."
	}

	normalizedSlug, slugErr := normalizeSlug(slug, title)
	if slugErr != "" {
		return Post{}, slugErr
	}

	tags, tagsErr := parsePostTags(tagsRaw)
	if tagsErr != "" {
		return Post{}, tagsErr
	}

	return Post{
		Slug:    normalizedSlug,
		Title:   title,
		Summary: summary,
		Content: content,
		Tags:    tags,
	}, ""
}

func normalizeSlug(raw, title string) (string, string) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		slug = slugify(title)
	}
	if slug == "" {
		return "", "Slug is required."
	}
	if len(slug) > maxPostSlugLen {
		return "", "Slug is too long."
	}
	if !slugPattern.MatchString(slug) {
		return "", "Slug may only contain lowercase letters, digits and dashes."
	}
	return slug, ""
}

func slugify(input string) string {
	replacer := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
	)
	normalized := replacer.Replace(strings.ToLower(input))

	var builder strings.Builder
	lastDash := false
	for _, r := range normalized {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && builder.Len() > 0 {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

func parsePostTags(raw string) ([]string, string) {
	if raw == "" {
		return nil, ""
	}

	seen := make(map[string]bool)
	var tags []string
	for _, part := range strings.Split(raw, ",") {
		tag := strings.ToUpper(strings.TrimSpace(part))
		if tag == "" || seen[tag] {
			continue
		}
		if len(tag) > maxPostTagLen {
			return nil, "Tags are too long."
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	if len(tags) > maxPostTagCount {
		return nil, "Too many tags."
	}
	return tags, ""
}
