package whitepaper

import (
	"encoding/json"
	"strconv"

	"github.com/bornholm/ghostwriter/pkg/article"
)

// FlexInt accepts both JSON integers and quoted strings (e.g. "1" or 1).
type FlexInt int

func (f *FlexInt) UnmarshalJSON(data []byte) error {
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*f = FlexInt(i)
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = FlexInt(i)
	return nil
}

// WhitePaperPlan is the top-level planning structure for a white paper.
type WhitePaperPlan struct {
	Title                    string     `json:"title" jsonschema:"required,description=The main title of the white paper"`
	Subtitle                 string     `json:"subtitle" jsonschema:"description=Optional subtitle"`
	TargetAudience           string     `json:"target_audience" jsonschema:"required,description=Intended audience for the white paper"`
	CentralArgument          string     `json:"central_argument" jsonschema:"required,description=The core thesis or central argument of the white paper in 1-2 sentences — the single idea every chapter must support"`
	Objectives               []string   `json:"objectives" jsonschema:"required,description=3-5 concrete objectives: what the reader will know or be able to do after reading"`
	ExecutiveSummaryGuidance []string   `json:"executive_summary_guidance" jsonschema:"required,description=Key messages for the executive summary (one per bullet)"`
	Parts                    []*Part    `json:"parts" jsonschema:"description=Optional part groupings — leave empty for flat chapter list"`
	Chapters                 []*Chapter `json:"chapters" jsonschema:"description=Flat list of chapters when not using parts"`
	AppendixTitles           []string   `json:"appendix_titles" jsonschema:"description=Titles of optional appendices"`
	Keywords                 []string   `json:"keywords" jsonschema:"required,description=Principal keywords for the white paper"`
	TotalWords               int        `json:"total_words" jsonschema:"required,description=Target total word count (5000–20000)"`
}

// Part groups chapters under a common theme.
type Part struct {
	Title    string     `json:"title" jsonschema:"required,description=Part title"`
	Chapters []*Chapter `json:"chapters" jsonschema:"required,description=Chapters within this part"`
}

// Chapter maps 1:1 to a Markdown output file.
type Chapter struct {
	ID          string            `json:"id" jsonschema:"description=Slug identifier (auto-generated if empty)"`
	Number      FlexInt           `json:"number" jsonschema:"required,description=Chapter number (1-based)"`
	Title       string            `json:"title" jsonschema:"required,description=Chapter title"`
	Description string            `json:"description" jsonschema:"required,description=Purpose and scope of this chapter"`
	KeyPoints   []string          `json:"key_points" jsonschema:"required,description=Key points this chapter must cover"`
	WordCount   int               `json:"word_count" jsonschema:"required,description=Target word count for this chapter"`
	Sections    []*ChapterSection `json:"sections" jsonschema:"description=Optional subsections within this chapter"`
}

// ChapterSection is a level-3 subsection (### heading) within a chapter.
type ChapterSection struct {
	Title       string   `json:"title" jsonschema:"required,description=Subsection title"`
	Description string   `json:"description" jsonschema:"description=Purpose of this subsection"`
	KeyPoints   []string `json:"key_points" jsonschema:"description=Key points for this subsection"`
	WordCount   int      `json:"word_count" jsonschema:"description=Target word count"`
}

// UnmarshalJSON accepts both a plain string (title only) and a full object.
// Models sometimes emit sections as a list of strings rather than objects.
func (s *ChapterSection) UnmarshalJSON(data []byte) error {
	var title string
	if err := json.Unmarshal(data, &title); err == nil {
		s.Title = title
		return nil
	}
	type chapterSectionAlias ChapterSection
	var alias chapterSectionAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*s = ChapterSection(alias)
	return nil
}

// ChapterContent is the written output for one chapter.
type ChapterContent struct {
	ChapterID string
	Number    int
	Title     string
	Content   string // Markdown body without the # heading (added by assembler)
	WordCount int
}

// CoherenceEditResult is the output of the coherence editing pass.
type CoherenceEditResult struct {
	ExecutiveSummary string            `json:"executive_summary"`
	Abstract         string            `json:"abstract"`
	Bibliography     []BibEntry        `json:"bibliography"`
	Appendices       []AppendixContent `json:"appendices"`
}

// BibEntry is a formatted bibliography entry.
type BibEntry struct {
	URL        string `json:"url"`
	Title      string `json:"title"`
	SourceType string `json:"source_type"`
}

// AppendixContent is an optional appendix section.
type AppendixContent struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// WhitePaper is the final assembled result.
type WhitePaper struct {
	Metadata     WhitePaperMetadata
	ChapterFiles []string // relative paths to per-chapter .md files
	Entrypoint   string   // path to index.md
}

// WhitePaperMetadata carries front-matter information.
type WhitePaperMetadata struct {
	Title    string
	Subtitle string
	Keywords []string
	Sources  []article.Source
}

// allChapters returns all chapters regardless of part grouping.
func (p *WhitePaperPlan) allChapters() []*Chapter {
	if len(p.Parts) > 0 {
		var chapters []*Chapter
		for _, part := range p.Parts {
			chapters = append(chapters, part.Chapters...)
		}
		return chapters
	}
	return p.Chapters
}
