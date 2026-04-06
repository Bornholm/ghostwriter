package whitepaper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosimple/slug"
	"github.com/pkg/errors"
)

// AssembleOptions configures the document assembly.
type AssembleOptions struct {
	OutputDir string
}

// Assemble writes all white paper files to outputDir and returns the WhitePaper result.
func Assemble(plan WhitePaperPlan, chapters []ChapterContent, coherence CoherenceEditResult, opts AssembleOptions) (WhitePaper, error) {
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return WhitePaper{}, errors.Wrap(err, "could not create output directory")
	}

	var chapterFiles []string

	// Write individual chapter files
	for _, ch := range chapters {
		filename := fmt.Sprintf("chapter-%02d-%s.md", ch.Number, slug.Make(ch.Title))
		path := filepath.Join(opts.OutputDir, filename)

		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n", ch.Title)
		b.WriteString(ch.Content)
		b.WriteString("\n")

		if err := os.WriteFile(path, []byte(b.String()), 0644); err != nil {
			return WhitePaper{}, errors.Wrapf(err, "could not write chapter file %s", filename)
		}
		chapterFiles = append(chapterFiles, filename)
	}

	// Write bibliography
	bibPath := filepath.Join(opts.OutputDir, "bibliography.md")
	if err := os.WriteFile(bibPath, []byte(buildBibliography(coherence.Bibliography)), 0644); err != nil {
		return WhitePaper{}, errors.Wrap(err, "could not write bibliography")
	}

	// Write appendices
	for i, appendix := range coherence.Appendices {
		filename := fmt.Sprintf("appendix-%02d-%s.md", i+1, slug.Make(appendix.Title))
		path := filepath.Join(opts.OutputDir, filename)
		content := fmt.Sprintf("# %s\n\n%s\n", appendix.Title, appendix.Content)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return WhitePaper{}, errors.Wrapf(err, "could not write appendix %s", filename)
		}
	}

	// Write index.md (amatl entrypoint)
	indexPath := filepath.Join(opts.OutputDir, "index.md")
	indexContent := buildIndex(plan, coherence, chapterFiles, len(coherence.Appendices))
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		return WhitePaper{}, errors.Wrap(err, "could not write index.md")
	}

	// Write plan.json so the fix command can reload the plan for coherence pass
	planJSON, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return WhitePaper{}, errors.Wrap(err, "could not serialize plan")
	}
	if err := os.WriteFile(filepath.Join(opts.OutputDir, "plan.json"), planJSON, 0644); err != nil {
		return WhitePaper{}, errors.Wrap(err, "could not write plan.json")
	}

	return WhitePaper{
		Metadata: WhitePaperMetadata{
			Title:    plan.Title,
			Subtitle: plan.Subtitle,
			Keywords: plan.Keywords,
		},
		ChapterFiles: chapterFiles,
		Entrypoint:   indexPath,
	}, nil
}

func buildIndex(plan WhitePaperPlan, coherence CoherenceEditResult, chapterFiles []string, appendixCount int) string {
	var b strings.Builder

	// YAML frontmatter
	b.WriteString("---\n")
	fmt.Fprintf(&b, "title: %q\n", plan.Title)
	if plan.Subtitle != "" {
		fmt.Fprintf(&b, "subtitle: %q\n", plan.Subtitle)
	}
	fmt.Fprintf(&b, "date: %q\n", time.Now().Format("2006-01-02"))
	b.WriteString("---\n\n")

	fmt.Fprintf(&b, "# %s\n\n", plan.Title)

	b.WriteString("## Table of contents\n\n")

	// Table of contents directive
	b.WriteString(":toc{minLevel=\"2\", maxLevel=\"3\"}\n\n")

	// Abstract
	if coherence.Abstract != "" {
		b.WriteString("## Abstract\n\n")
		b.WriteString(coherence.Abstract)
		b.WriteString("\n\n")
	}

	// Executive Summary
	if coherence.ExecutiveSummary != "" {
		b.WriteString("## Executive Summary\n\n")
		b.WriteString(coherence.ExecutiveSummary)
		b.WriteString("\n\n")
	}

	// Chapter includes
	for _, f := range chapterFiles {
		fmt.Fprintf(&b, ":include{url=%q, shiftHeadings=\"1\"}\n\n", f)
	}

	// Bibliography
	b.WriteString(":include{url=\"bibliography.md\", shiftHeadings=\"1\"}\n\n")

	// Appendices
	for i, appendix := range coherence.Appendices {
		filename := fmt.Sprintf("appendix-%02d-%s.md", i+1, slug.Make(appendix.Title))
		fmt.Fprintf(&b, ":include{url=%q, shiftHeadings=\"1\"}\n\n", filename)
	}
	_ = appendixCount

	return b.String()
}

func buildBibliography(entries []BibEntry) string {
	if len(entries) == 0 {
		return "# Bibliography\n\nNo sources available.\n"
	}

	var b strings.Builder
	b.WriteString("# Bibliography\n\n")

	for i, e := range entries {
		title := e.Title
		if title == "" {
			title = e.URL
		}
		if e.URL != "" {
			fmt.Fprintf(&b, "%d. [%s](%s)\n", i+1, title, e.URL)
		} else {
			fmt.Fprintf(&b, "%d. %s\n", i+1, title)
		}
	}
	b.WriteString("\n")
	return b.String()
}
