package whitepaper

import "github.com/bornholm/genai/agent"

const (
	// EventTypePhase is emitted when a pipeline phase starts or ends.
	EventTypePhase agent.EventType = "whitepaper.phase"
	// EventTypeChapterStart is emitted when a chapter begins writing.
	EventTypeChapterStart agent.EventType = "whitepaper.chapter_start"
	// EventTypeChapterDone is emitted when a chapter is fully written and edited.
	EventTypeChapterDone agent.EventType = "whitepaper.chapter_done"
)

// PhaseData carries information about a pipeline phase transition.
type PhaseData struct {
	Name string
	Done bool
	Info string // optional extra info for done events
}

// ChapterStartData is emitted when writing begins for a chapter.
type ChapterStartData struct {
	Number int
	Total  int
	Title  string
	Target int // target word count
}

// ChapterDoneData is emitted when a chapter is fully written and edited.
type ChapterDoneData struct {
	Number    int
	Total     int
	Title     string
	WordCount int
}
