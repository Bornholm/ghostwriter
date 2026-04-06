package whitepaper

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	chapterFileRe  = regexp.MustCompile(`^chapter-(\d+)-`)
	appendixFileRe = regexp.MustCompile(`^appendix-(\d+)-`)
)

// AnnotatedFile holds the parsed content of a whitepaper chapter file
// that contains > EDITOR: ... reviewer annotations.
type AnnotatedFile struct {
	FilePath     string
	Title        string
	Number       int
	Annotations  []string
	CleanContent string
}

// ParseAnnotatedFile reads a markdown chapter file and extracts
// any > EDITOR: ... annotations, returning the clean content separately.
func ParseAnnotatedFile(filePath string) (AnnotatedFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return AnnotatedFile{}, fmt.Errorf("could not read file %s: %w", filePath, err)
	}

	base := filepath.Base(filePath)
	number := parseFileNumber(base)

	var (
		title        string
		annotations  []string
		cleanLines   []string
		prevWasBlank bool
	)

	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Extract title from first # heading
		if title == "" && strings.HasPrefix(trimmed, "# ") {
			title = strings.TrimPrefix(trimmed, "# ")
			continue
		}

		// Capture > EDITOR: annotations
		if strings.HasPrefix(trimmed, "> EDITOR:") {
			annotation := strings.TrimSpace(strings.TrimPrefix(trimmed, "> EDITOR:"))
			annotations = append(annotations, annotation)
			continue
		}

		// Suppress duplicate blank lines introduced by removed annotation lines
		isBlank := trimmed == ""
		if isBlank && prevWasBlank {
			continue
		}
		prevWasBlank = isBlank

		cleanLines = append(cleanLines, line)
	}

	if err := scanner.Err(); err != nil {
		return AnnotatedFile{}, fmt.Errorf("could not scan file %s: %w", filePath, err)
	}

	// Trim leading/trailing blank lines from clean content
	cleanContent := strings.TrimSpace(strings.Join(cleanLines, "\n"))

	return AnnotatedFile{
		FilePath:     filePath,
		Title:        title,
		Number:       number,
		Annotations:  annotations,
		CleanContent: cleanContent,
	}, nil
}

// loadChaptersFromDir reads all chapter-*.md files from a directory,
// strips any > EDITOR: annotations, and returns ChapterContent sorted by number.
func loadChaptersFromDir(dir string) ([]ChapterContent, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "chapter-*.md"))
	if err != nil {
		return nil, fmt.Errorf("could not glob chapter files: %w", err)
	}

	chapters := make([]ChapterContent, 0, len(matches))
	for _, path := range matches {
		af, err := ParseAnnotatedFile(path)
		if err != nil {
			return nil, err
		}

		base := filepath.Base(path)
		chapterID := strings.TrimSuffix(base, ".md")

		chapters = append(chapters, ChapterContent{
			ChapterID: chapterID,
			Number:    af.Number,
			Title:     af.Title,
			Content:   af.CleanContent,
			WordCount: countWords(af.CleanContent),
		})
	}

	// Sort by chapter number
	for i := 1; i < len(chapters); i++ {
		for j := i; j > 0 && chapters[j].Number < chapters[j-1].Number; j-- {
			chapters[j], chapters[j-1] = chapters[j-1], chapters[j]
		}
	}

	return chapters, nil
}


func parseFileNumber(filename string) int {
	for _, re := range []*regexp.Regexp{chapterFileRe, appendixFileRe} {
		if m := re.FindStringSubmatch(filename); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n
		}
	}
	return 0
}
