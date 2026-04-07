# Ghostwriter

Outil CLI pour générer des livres blancs (white papers) complets et sourcés à partir d'un sujet, en utilisant des LLMs.

## Fonctionnement

Le pipeline multi-agents produit un répertoire de fichiers Markdown prêts à être publiés :

1. **Recherche** — génère des requêtes de recherche, scrape le web, indexe les documents dans une base de connaissances
2. **Planification** — crée un plan structuré (titre, chapitres, points clés, nombre de mots cibles)
3. **Rédaction** — un agent par chapitre, avec accès à la base de connaissances
4. **Édition** — révision de chaque chapitre (N rounds configurable)
5. **Cohérence** — génère l'abstract, le résumé exécutif, la bibliographie et les annexes
6. **Enrichissement** — insertion de liens de citation et de diagrammes Mermaid
7. **Assemblage** — production des fichiers finaux (`chapter-*.md`, `index.md`, `bibliography.md`, `plan.json`)
8. **Rendu** (optionnel) — conversion HTML/PDF via amatl

## Installation

```bash
go build -o ghostwriter ./cmd/ghostwriter
```

## Configuration

Créer un fichier `.env` :

```env
GHOSTWRITER_CHAT_COMPLETION_PROVIDER=openrouter   # openai | openrouter | mistral
GHOSTWRITER_CHAT_COMPLETION_OPENROUTER_API_KEY=<clé>
GHOSTWRITER_CHAT_COMPLETION_OPENROUTER_BASE_URL=https://openrouter.ai/api/v1
GHOSTWRITER_CHAT_COMPLETION_OPENROUTER_MODEL=google/gemini-2.5-flash

GHOSTWRITER_EMBEDDINGS_PROVIDER=openrouter
GHOSTWRITER_EMBEDDINGS_OPENROUTER_API_KEY=
GHOSTWRITER_EMBEDDINGS_OPENROUTER_BASE_URL=https://openrouter.ai/api/v1
GHOSTWRITER_EMBEDDINGS_OPENROUTER_MODEL=qwen/qwen3-embedding-8b
```

## Utilisation

### Générer un livre blanc

```bash
go run ./cmd/ghostwriter whitepaper --subject "Votre sujet ici"
```

Options principales :

| Flag                    | Alias | Défaut        | Description                                                       |
| ----------------------- | ----- | ------------- | ----------------------------------------------------------------- |
| `--subject`             | `-s`  | —             | Sujet du livre blanc (requis)                                     |
| `--subject-file`        | `-sf` | —             | Chemin vers un fichier contenant le sujet                         |
| `--output-dir`          | `-o`  | slug du sujet | Dossier de sortie                                                 |
| `--target-words`        | `-t`  | `10000`       | Nombre de mots cibles                                             |
| `--style-guide`         | `-g`  | —             | Fichier guide de style (Markdown)                                 |
| `--research-depth`      | `-d`  | `deep`        | Profondeur de recherche : `basic`, `deep`, `deep_web`, `academic` |
| `--files`               | `-f`  | —             | Fichiers locaux à injecter dans la base de connaissances          |
| `--additional-context`  | `-c`  | —             | Fichier de contexte additionnel transmis aux agents               |
| `--max-review-rounds`   |       | `2`           | Nombre de rounds d'édition par chapitre (min 1)                   |
| `--parts`               |       | `false`       | Activer le regroupement des chapitres en parties                  |
| `--output-html`         |       | —             | Chemin de sortie HTML                                             |
| `--output-pdf`          |       | —             | Chemin de sortie PDF                                              |
| `--corpus-storage-path` |       | `.corpus`     | Répertoire de stockage Corpus (base de connaissances persistante) |

**Exemple complet :**

```bash
go run ./cmd/ghostwriter whitepaper \
  --subject "Responsabilité numérique dans les projets IT" \
  --target-words 15000 \
  --style-guide ./style/fr.md \
  --research-depth deep \
  --output-dir ./output/whitepaper \
  --output-pdf ./output/whitepaper.pdf
```

### Corriger les annotations

Après avoir lu et annoté les chapitres générés avec des commentaires `> EDITOR: ...`, relancez le pipeline d'édition :

```bash
go run ./cmd/ghostwriter fix --dir ./output/whitepaper
```

Options :

| Flag                    | Alias | Description                                                    |
| ----------------------- | ----- | -------------------------------------------------------------- |
| `--dir`                 | `-d`  | Répertoire du livre blanc (requis)                             |
| `--style-guide`         | `-g`  | Guide de style                                                 |
| `--additional-context`  | `-c`  | Contexte additionnel                                           |
| `--enrich`              |       | Forcer le pass d'enrichissement même sans annotations          |
| `--corpus-storage-path` |       | Chemin vers la base Corpus (détecté automatiquement si absent) |

## Architecture

```
pkg/
├── article/          # Base partagée : ResearchAgent, KnowledgeBase, types
├── whitepaper/       # Pipeline complet : planner, writer, editor, coherence, assembler
├── scraper/          # Backends de scraping : HTTP, Surf, Chromedp
├── search/           # Clients de recherche : DuckDuckGo, Searx, Google, Meta
├── tool/             # Outils LLM : web_search, scrape_webpage, query_document
└── knowledgebase/    # Adaptateur Corpus → KnowledgeBase

internal/
├── command/          # Commandes CLI : whitepaper, fix
├── build/            # Version
└── logx/             # Handler slog avec contexte
```

### Base de connaissances

Deux backends disponibles :

- **Bleve** (défaut) — index full-text en mémoire, sans configuration
- **Corpus** — backend vectoriel persistant, activé dès que `--corpus-storage-path` pointe vers un répertoire existant

## Build release

```bash
make goreleaser
```
