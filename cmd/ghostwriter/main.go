package main

import (
	"github.com/bornholm/ghostwriter/internal/build"
	"github.com/bornholm/ghostwriter/internal/command"
	"github.com/bornholm/ghostwriter/internal/command/write"

	_ "github.com/bornholm/genai/llm/provider/all"
)

func main() {
	command.Main(
		"ghostwriter", build.Version, "write/edit articles with LLMs",
		write.Root(),
	)
}
