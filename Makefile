# Override on the command line, e.g.: make ask CHAT_MODEL=qwen3:14b
DB          ?= data/index.db
EMBED_MODEL ?= nomic-embed-text
CHAT_MODEL  ?= qwen3:30b-a3b
CHAT_API    ?= ollama
CHAT_URL    ?=
CHAT_KEY    ?=
OLLAMA_URL  ?= http://localhost:11434

.DEFAULT_GOAL := help
.PHONY: help setup deps models build test vet ask clean reset

help:
	@echo "emails-rag — local RAG over PDFs and mbox archives"
	@echo ""
	@echo "One-time setup:"
	@echo "  make setup         Install brew deps and pull Ollama models"
	@echo "  make deps          Just install brew deps (poppler, ocrmypdf, ollama)"
	@echo "  make models        Just pull the Ollama models"
	@echo ""
	@echo "Daily use:"
	@echo "  make ask           Run the interactive REPL"
	@echo "  make build         go build ./..."
	@echo "  make test          go test ./..."
	@echo "  make vet           go vet ./..."
	@echo ""
	@echo "Maintenance:"
	@echo "  make clean         Remove compiled binaries"
	@echo "  make reset         Delete the SQLite index (asks first)"
	@echo ""
	@echo "Indexing isn't make-wrapped because flags vary per run. Example:"
	@echo "  go run ./cmd/index -pdf-dir ~/Documents/contracts -mbox ~/mail/work.mbox"
	@echo ""
	@echo "Override model / db with env-style vars:"
	@echo "  make ask CHAT_MODEL=qwen3:14b DB=other.db"
	@echo ""
	@echo "Talk to a remote vLLM / OpenAI-compatible server:"
	@echo "  make ask CHAT_API=openai CHAT_URL=http://192.168.191.4:8000 CHAT_MODEL=Qwen/Qwen3-32B"

setup: deps models
	@echo ""
	@echo "Setup complete. Index something, then run: make ask"

deps:
	@command -v brew >/dev/null 2>&1 || { \
		echo "Homebrew is required. Install from https://brew.sh and re-run."; \
		exit 1; \
	}
	brew install poppler ocrmypdf
	@command -v ollama >/dev/null 2>&1 || brew install ollama
	@echo "Tip: the Ollama desktop app auto-starts the server in the menu bar."
	@echo "     If you installed the CLI only, run 'ollama serve' in another terminal."

models:
	@curl -fsS $(OLLAMA_URL)/api/version >/dev/null 2>&1 || { \
		echo "Ollama not reachable at $(OLLAMA_URL)."; \
		echo "Launch the Ollama app, or run 'ollama serve' in another terminal."; \
		exit 1; \
	}
	ollama pull $(EMBED_MODEL)
	ollama pull $(CHAT_MODEL)

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

ask:
	@curl -fsS $(OLLAMA_URL)/api/version >/dev/null 2>&1 || { \
		echo "Ollama (embeddings) not reachable at $(OLLAMA_URL). Start the app first."; \
		exit 1; \
	}
	@if [ "$(CHAT_API)" = "openai" ] && [ -n "$(CHAT_URL)" ]; then \
		curl -fsS $(CHAT_URL)/v1/models >/dev/null 2>&1 || { \
			echo "Chat backend not reachable at $(CHAT_URL). Is vLLM running?"; \
			exit 1; \
		}; \
	fi
	go run ./cmd/ask \
		-db $(DB) \
		-embed-model $(EMBED_MODEL) \
		-chat-model $(CHAT_MODEL) \
		-chat-api $(CHAT_API) \
		$(if $(CHAT_URL),-chat-url $(CHAT_URL)) \
		$(if $(CHAT_KEY),-chat-key $(CHAT_KEY))

clean:
	rm -f emails-rag ask index cmd/ask/ask cmd/index/index

reset:
	@read -p "Delete $(DB) and all indexed data under data/? [y/N] " ans; \
	case $$ans in \
		[yY]*) rm -rf data/ && echo "Reset.";; \
		*) echo "Aborted.";; \
	esac
