package whitepaper

import (
	"testing"
)

func TestValidatePlan(t *testing.T) {
	t.Run("valid flat plan", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Title: "Test Paper",
			Chapters: []*Chapter{
				{ID: "intro", Number: 1, Title: "Introduction", WordCount: 500},
				{ID: "body", Number: 2, Title: "Body", WordCount: 1000},
			},
		}
		if err := validatePlan(plan); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("empty chapters", func(t *testing.T) {
		plan := &WhitePaperPlan{Title: "Empty"}
		if err := validatePlan(plan); err == nil {
			t.Error("expected error for plan with no chapters")
		}
	})

	t.Run("zero word count", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Chapters: []*Chapter{
				{ID: "ch1", Number: 1, Title: "Chapter 1", WordCount: 0},
			},
		}
		if err := validatePlan(plan); err == nil {
			t.Error("expected error for chapter with zero word count")
		}
	})

	t.Run("negative word count", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Chapters: []*Chapter{
				{ID: "ch1", Number: 1, Title: "Chapter 1", WordCount: -100},
			},
		}
		if err := validatePlan(plan); err == nil {
			t.Error("expected error for chapter with negative word count")
		}
	})

	t.Run("empty chapter ID", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Chapters: []*Chapter{
				{ID: "", Number: 1, Title: "Chapter 1", WordCount: 500},
			},
		}
		if err := validatePlan(plan); err == nil {
			t.Error("expected error for chapter with empty ID")
		}
	})

	t.Run("duplicate chapter IDs", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Chapters: []*Chapter{
				{ID: "dup", Number: 1, Title: "Chapter 1", WordCount: 500},
				{ID: "dup", Number: 2, Title: "Chapter 2", WordCount: 500},
			},
		}
		if err := validatePlan(plan); err == nil {
			t.Error("expected error for duplicate chapter IDs")
		}
	})

	t.Run("plan with parts", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Parts: []*Part{
				{
					Title: "Part I",
					Chapters: []*Chapter{
						{ID: "ch1", Number: 1, Title: "Chapter 1", WordCount: 500},
					},
				},
			},
		}
		if err := validatePlan(plan); err != nil {
			t.Errorf("expected no error, got: %v", err)
		}
	})

	t.Run("duplicate IDs across parts", func(t *testing.T) {
		plan := &WhitePaperPlan{
			Parts: []*Part{
				{Chapters: []*Chapter{{ID: "dup", Number: 1, Title: "Ch1", WordCount: 500}}},
				{Chapters: []*Chapter{{ID: "dup", Number: 2, Title: "Ch2", WordCount: 500}}},
			},
		}
		if err := validatePlan(plan); err == nil {
			t.Error("expected error for duplicate IDs across parts")
		}
	})
}

func TestGenerateChapterID(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"Introduction", "introduction"},
		{"Hello World", "hello_world"},
		{"Foo & Bar!", "foo__bar"},
		{"123 Numbers", "123_numbers"},
		{"  Spaces  ", "__spaces__"},
	}
	for _, tc := range cases {
		got := generateChapterID(tc.title)
		if got != tc.want {
			t.Errorf("generateChapterID(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
