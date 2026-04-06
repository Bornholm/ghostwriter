package main

import (
	"github.com/bornholm/ghostwriter/internal/build"
	"github.com/bornholm/ghostwriter/internal/command"
	"github.com/bornholm/ghostwriter/internal/command/fix"
	"github.com/bornholm/ghostwriter/internal/command/whitepaper"

	_ "github.com/bornholm/genai/llm/provider/all"
)

func main() {
	command.Main(
		"ghostwriter", build.Version, "write/edit articles with LLMs",
		whitepaper.Root(),
		fix.Root(),
	)
}
