# Ghostwriter

A little tool to write structured and sourced Markdown documents with LLMs.

## Build

```bash
go build -o bin/ghostwriter ./cmd/ghostwriter
```

## Usage

1. Create `.env` file

   ```shell
   LLM_API_KEY=<YOUR_API_KEY>
   LLM_API_BASE_URL=<LLM_API_BASE_URL>
   LLM_MODEL=<LLM_MODEL_IDENTIFIER
   ```

   For example, with a local [ollama](https://ollama.com/) instance:

   ```shell
   LLM_API_KEY=
   LLM_API_BASE_URL=http://127.0.0.1:11434/v1/
   LLM_MODEL=mistral:7b
   ```

2. Create a 'project' file in YAML format:

   ```yaml
   topic: A description of what the document should talk about
   language: english|french|etc
   corpus:
     - resource: An URL to a resource to use as a source of informations
       type: files|website|url
   ```

   For example:

   ```yaml
   topic: Une introduction aux grands modèles de langage.
   language: french
   corpus:
     - resource: https://fr.wikipedia.org/wiki/Grand_mod%C3%A8le_de_langage
   ```

3. Generate your document:

   ```bash
   ./bin/ghostwriter ./path/to/project.yml
   ```

   Youd document will be written to `<project_filename>_<model>_<timestamp>.md`

   [Output sample for the project example with the `ministral-8b-latest` model](https://gist.github.com/Bornholm/6d3ce33a4b884befc90bbc00aef5a557).
