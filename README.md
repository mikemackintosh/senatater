# emails-rag

Local retrieval-augmented question answering over PDFs and MBOX email archives. Runs entirely on a local Ollama server with a SQLite-backed vector index. **No data leaves the machine.**

This README is meant to double as a personal runbook: every command you need to set up, index, query, troubleshoot, and push the project lives here.

---

## Table of contents

1. [What this is](#what-this-is)
2. [Quick start with Make](#quick-start-with-make)
3. [Requirements](#requirements)
4. [First-time installation](#first-time-installation)
5. [First run (smoke test)](#first-run-smoke-test)
6. [Daily usage](#daily-usage)
7. [Command reference](#command-reference)
8. [Remote chat backend (vLLM / DGX Spark)](#remote-chat-backend-vllm--dgx-spark)
9. [How it works](#how-it-works)
10. [Operational notes](#operational-notes)
11. [Testing](#testing)
12. [Troubleshooting](#troubleshooting)
13. [Pushing to GitHub](#pushing-to-github)
14. [Extending](#extending)
15. [Project layout](#project-layout)

---

## What this is

Two small Go CLIs:

- **`cmd/index`** ingests PDFs (any directory tree) and `.mbox` archives. For each source, it extracts text, splits it into ~1200-character overlapping chunks, embeds each chunk via a local Ollama embedding model, and stores everything in a SQLite database.
- **`cmd/ask`** is an interactive REPL. You type a question, it embeds the question, searches the SQLite index by cosine similarity, takes the top-k chunks, and asks a local Ollama chat model to answer using only that retrieved context. The answer streams to the terminal; sources with similarity scores are listed after.

Everything is local: embeddings, vector store, LLM. There is no telephoning home. The SQLite file *is* the index — copy it, back it up, delete it like any other file.

---

## Quick start with Make

If you have Homebrew installed and the Ollama desktop app running, this is the entire bootstrap:

```bash
git clone git@github.com:<your-user>/emails-rag.git
cd emails-rag
make setup           # brew install poppler ocrmypdf + ollama pull both models
go run ./cmd/index -pdf-dir ~/Documents/contracts
make ask
```

All Make targets:

| Target | What it does |
|---|---|
| `make setup` | `deps` + `models` — full one-time bootstrap |
| `make deps` | `brew install poppler ocrmypdf` (+ `ollama` if not already on `PATH`) |
| `make models` | `ollama pull` both the embedder and chat model |
| `make build` | `go build ./...` |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make ask` | Run the interactive REPL (pre-checks that Ollama is reachable) |
| `make clean` | Remove compiled binaries |
| `make reset` | Delete `data/` after a `[y/N]` prompt |
| `make help` | Print the same list inline |

Any of these env-style variables can be overridden inline:

```bash
make ask CHAT_MODEL=qwen3:14b
make models EMBED_MODEL=bge-m3
make ask DB=other.db OLLAMA_URL=http://localhost:11435

# Talk to a remote vLLM / OpenAI-compatible server instead of local Ollama:
make ask CHAT_API=openai CHAT_URL=http://192.168.191.4:8000 CHAT_MODEL=Qwen/Qwen3-32B
```

See [Remote chat backend (vLLM / DGX Spark)](#remote-chat-backend-vllm--dgx-spark) for the full server-side setup.

Indexing is not Make-wrapped because flag combinations vary per run — use `go run ./cmd/index ...` with whichever `-pdf-dir`/`-mbox` flags you need.

The rest of this README explains each piece in detail and is the place to look when something doesn't work.

---

## Requirements

### Hardware

- **macOS on Apple Silicon** is the tested target. Built and exercised on M4 / 32 GB.
- Roughly **18 GB of free RAM** while `cmd/ask` is generating, because the default chat model (`qwen3:30b-a3b`) is ~17 GB on disk and resident at Q4 quantization. The smaller `qwen3:14b` works on 16 GB machines.
- **~25 GB free disk** for models (`qwen3:30b-a3b` ~17 GB + `nomic-embed-text` ~270 MB) plus whatever your index and source archives need.

### Software

- **Go 1.24+**
- **[Ollama](https://ollama.com)** running on `localhost:11434`
- **`pdftotext`** (from poppler) for PDF text extraction
- **`ocrmypdf`** for the scanned-PDF fallback path
- **Homebrew** is the easiest way to install the system deps

---

## First-time installation

These are one-time steps. Skip any tool you already have. If you just want the fast path, jump to [Quick start with Make](#quick-start-with-make); the section below explains what each Make target is actually doing.

### 1. Install Homebrew (if you don't have it)

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Verify:

```bash
brew --version
```

### 2. Install Go

```bash
brew install go
go version  # should print 1.24 or later
```

If you need a newer Go than Homebrew ships, download from <https://go.dev/dl/> and ensure `go` is in your `PATH`.

### 3. Install system dependencies

```bash
brew install poppler ocrmypdf
```

Or, equivalently, `make deps` (which also installs `ollama` if it isn't already on `PATH`).

Verify both are on `PATH`:

```bash
which pdftotext   # /opt/homebrew/bin/pdftotext
which ocrmypdf    # /opt/homebrew/bin/ocrmypdf
pdftotext -v 2>&1 | head -1
ocrmypdf --version
```

### 4. Install Ollama

Either download the desktop app from <https://ollama.com> (recommended on macOS — it auto-starts the server and runs in the menu bar), or:

```bash
brew install ollama
```

Start the server if it's not running:

```bash
# If installed via brew, run it manually:
ollama serve
# (or just launch the Ollama desktop app)
```

Verify it's reachable:

```bash
curl -s http://localhost:11434/api/version
# {"version":"0.x.y"}
```

### 5. Pull the models

These are big downloads — do them on a stable connection, not on a metered hotspot.

```bash
# Chat model — about 17 GB. Default for cmd/ask.
ollama pull qwen3:30b-a3b

# Embedding model — about 270 MB. Used by both cmd/index and cmd/ask.
ollama pull nomic-embed-text
```

Or, equivalently, `make models` — pulls whichever models `EMBED_MODEL` and `CHAT_MODEL` point at (defaults above).

Smaller / faster alternatives, useful when RAM or bandwidth is tight:

```bash
ollama pull qwen3:14b          # ~9 GB chat model
ollama pull bge-m3             # ~1.2 GB multilingual embedder, longer context

# Or via make:
make models CHAT_MODEL=qwen3:14b EMBED_MODEL=bge-m3
```

Verify the pulls:

```bash
ollama list
```

### 6. Get the project

```bash
git clone <your-github-url>.git
cd emails-rag
go mod tidy
```

`go mod tidy` pulls the single Go dependency (`github.com/mattn/go-sqlite3`).

### 7. Verify the build and tests

```bash
go build ./...
go test ./...
# Or: make build test
```

You should see `ok` for each `internal/...` package. If anything fails, fix that before indexing.

---

## First run (smoke test)

Goal: index one PDF and ask one question end-to-end. About two minutes after the models are pulled.

```bash
# 1. Make sure Ollama is up.
curl -s http://localhost:11434/api/version

# 2. Put a small PDF somewhere.
mkdir -p sample-pdfs
# Drop any short text-bearing PDF into ./sample-pdfs/

# 3. Index it.
go run ./cmd/index -pdf-dir ./sample-pdfs

# You should see lines like:
#   time=... level=INFO msg="indexing pdf" path=sample-pdfs/foo.pdf chunks=42
#   time=... level=INFO msg="indexing complete" total_chunks=42

# 4. Ask a question.
go run ./cmd/ask
# > what is this document about?
# (tokens stream here)
#
# Sources:
#   [1] sample-pdfs/foo.pdf (score=0.812)
#   ...
# Empty line exits.
```

If this works, you're operational. Re-running step 3 on the same directory is a no-op because `cmd/index` skips sources whose path is already in the database.

---

## Daily usage

### Make sure Ollama is running

The desktop app handles this for you — check for the icon in the menu bar. If you installed via brew you may need to keep `ollama serve` running in a terminal or set it up as a launch agent.

A quick liveness check:

```bash
curl -fsS http://localhost:11434/api/version || echo "OLLAMA IS DOWN"
```

### Indexing

The index lives at `./data/index.db` by default. The first run creates `./data/` and the schema.

**One PDF directory:**

```bash
go run ./cmd/index -pdf-dir ~/Documents/contracts
```

Walks the directory recursively; only `.pdf` files (case-insensitive) are indexed.

**Multiple PDF directories + multiple mbox archives in one run:**

```bash
go run ./cmd/index \
  -pdf-dir ~/Documents/contracts \
  -pdf-dir ~/Documents/research \
  -mbox ~/mail/personal.mbox \
  -mbox ~/mail/work.mbox
```

Both flags are repeatable. A failure on any one path (broken file, OCR crash, parser issue) is logged but does not abort the rest of the run.

**Re-running is safe.** Dedup works differently for the two source types:

- **PDFs dedup by file path.** A subsequent indexer run on the same path is skipped entirely (`level=INFO msg="skipping already-indexed pdf" path=...`). To re-ingest a changed PDF, remove its rows from SQLite first.
- **MBOX archives dedup per message** by the RFC 5322 `Message-ID` header (with a `From`+`Date`+`Subject`+body-prefix fingerprint as fallback for messages without one). This means you can re-run the indexer on a growing mbox and only the new mail is ingested:

  ```text
  level=INFO msg="indexed mbox" path=... messages_indexed=42 messages_skipped=8410 chunks=137
  ```

**Scanned PDFs are handled automatically.** If `pdftotext` returns near-empty output (fewer than 100 visible characters), the indexer transparently runs `ocrmypdf --skip-text --quiet` against a temp PDF, then re-extracts. This is slow per page (OCR is expensive), but it only happens when needed.

### Asking questions

```bash
go run ./cmd/ask
# Or: make ask
```

`make ask` adds a pre-flight check that Ollama is reachable and forwards `DB`, `EMBED_MODEL`, `CHAT_MODEL` as flags. Override them inline, e.g. `make ask CHAT_MODEL=qwen3:14b`.

The REPL prints:

```text
Ask away. Empty line exits. Ctrl-C cancels the current query.
Prefix with "source:pdf " or "source:mbox " to filter a single query.

>
```

Tokens stream as the chat model generates. After the answer completes, a `Sources:` block lists the retrieved chunks with similarity scores. For mbox hits the line includes the subject and date pulled from the email headers. Each citation is followed by an ~100-character preview of the matched chunk so you can sanity-check what retrieval landed on:

```text
Sources:
  [1] /Users/me/mail/personal.mbox: "Re: Q3 numbers" — Mon, 15 Mar 2024 09:12:00 -0400 (score=0.812)
      looking at the breakdown for Q3, we hit 102% of target despite the supply chain hiccup in...
  [2] /Users/me/Documents/contracts/lease.pdf (score=0.749)
      Subletting requires written consent from Landlord and shall not be unreasonably withheld...
```

**Empty line exits.** **`Ctrl-C` cancels the current query** without exiting the program — the streaming output stops, you see `(cancelled)`, and you're back at the prompt. A second `Ctrl-C` at the empty prompt exits.

### Filtering by source type

Restrict an entire session to one corpus:

```bash
go run ./cmd/ask -source mbox       # email-only session
go run ./cmd/ask -source pdf        # PDF-only session
```

Override per-query inline (the prefix beats the session default):

```text
> source:pdf what does my lease say about subletting?
> source:mbox when did Sarah last mention the Q3 numbers?
> source:web what's the weather?      # unknown type → treated as plain text
```

Inline prefix matches exactly `source:pdf` or `source:mbox`, case-insensitive, followed by whitespace and the actual question.

### Switching models

For lower memory pressure or faster startup:

```bash
go run ./cmd/ask -chat-model qwen3:14b
go run ./cmd/ask -embed-model bge-m3
```

**Important:** if you switch the *embedding* model after indexing, your existing embeddings are no longer comparable to the new query embeddings — retrieval will be garbage. Either re-index from scratch, or keep using the same embedding model that built the database.

---

## Command reference

### `go run ./cmd/index`

Bulk ingest PDFs and MBOX archives into the SQLite index.

| Flag | Type | Default | Notes |
|---|---|---|---|
| `-db` | string | `data/index.db` | Path to the SQLite index file. Parent dir is created if missing. |
| `-embed-model` | string | `nomic-embed-text` | Ollama embedding model name. Must match the model used at query time. |
| `-pdf-dir` | string (repeatable) | — | Directory to walk recursively for `.pdf` files. Pass once per directory. |
| `-mbox` | string (repeatable) | — | Path to an `.mbox` file. Pass once per file. |

Behavior notes:

- PDFs dedup by file path (same path = full skip). MBOX archives dedup per message by `Message-ID` (with a content-fingerprint fallback) so re-running on a growing mbox is safe and incremental.
- PDFs are extracted via `pdftotext -layout`. If output is below the OCR threshold (100 visible chars), `ocrmypdf` runs on a temp copy and the result is re-extracted.
- MBOX messages are parsed as RFC 5322 with full MIME walking: `multipart/alternative` prefers `text/plain`; HTML parts are stripped; `quoted-printable` and `base64` transfer encodings are decoded; RFC 2047 encoded headers (`=?UTF-8?B?...?=`) are decoded.
- Email metadata (`from`, `to`, `subject`, `date`) is stored alongside each chunk and surfaced in the `ask` Sources block.

### `go run ./cmd/ask`

Interactive RAG REPL.

| Flag | Type | Default | Notes |
|---|---|---|---|
| `-db` | string | `data/index.db` | Path to the SQLite index file. |
| `-embed-model` | string | `nomic-embed-text` | Ollama embedding model for the query side. Must match the model used at index time. |
| `-chat-model` | string | `qwen3:30b-a3b` | Ollama chat model that generates the answer. |
| `-k` | int | `6` | Number of chunks retrieved per query and passed to the chat model. |
| `-source` | string | `""` | Session default filter: `pdf`, `mbox`, or empty for all types. Inline `source:` prefix overrides this per query. |
| `-chat-api` | string | `ollama` | Chat backend: `ollama` (default, local) or `openai` (any OpenAI-compatible server — vLLM, LM Studio, OpenAI itself). |
| `-chat-url` | string | `""` | Chat server base URL. Empty uses each backend's default: `http://localhost:11434` for Ollama, `http://localhost:8000` for OpenAI-compatible. **Do not include `/v1`** for the OpenAI variant — the client appends it. |
| `-chat-key` | string | `""` | Bearer token sent as `Authorization: Bearer ...`. Only honored by the OpenAI backend; vLLM by default doesn't require auth. |

---

## Remote chat backend (vLLM / DGX Spark)

For large or fast chat models you can run vLLM (or any OpenAI-compatible server) on a separate box and point `cmd/ask` at it. Embeddings stay local on the Mac via Ollama — embedding is small and fast, no reason to add a network hop.

This section walks through running **Qwen3-32B** on an **NVIDIA DGX Spark** at `192.168.191.4`, then connecting from the Mac.

### On the DGX Spark

The DGX Spark runs DGX OS (Ubuntu-based) with NVIDIA Container Toolkit pre-installed. SSH in, then:

#### 1. Pull the vLLM Docker image

```bash
ssh user@192.168.191.4
docker pull vllm/vllm-openai:latest
```

#### 2. (Optional) Pre-cache the model

vLLM will pull the weights on first run, but pre-fetching makes the first launch fast and surfaces any HuggingFace auth issues immediately. The Qwen3 models on the [Qwen org](https://huggingface.co/Qwen) are public, so no token is needed:

```bash
mkdir -p ~/hf-cache
docker run --rm \
  -v ~/hf-cache:/root/.cache/huggingface \
  --entrypoint huggingface-cli \
  vllm/vllm-openai:latest \
  download Qwen/Qwen3-32B
```

#### 3. Launch the server

In a `tmux` session so it survives logout:

```bash
tmux new -s vllm
docker run --rm --runtime nvidia --gpus all \
  -v ~/hf-cache:/root/.cache/huggingface \
  -p 8000:8000 \
  --ipc=host \
  --name vllm-qwen3 \
  vllm/vllm-openai:latest \
  --model Qwen/Qwen3-32B \
  --host 0.0.0.0 \
  --port 8000 \
  --max-model-len 16384 \
  --gpu-memory-utilization 0.9
```

Notable flags:

- `--host 0.0.0.0` is critical — without it the server only listens on the container's loopback and the Mac can't reach it.
- `--max-model-len 16384` is generous; raise it if you intend to use longer contexts and lower it if you hit OOM.
- `--gpu-memory-utilization 0.9` leaves a small headroom; on the DGX Spark's 128 GB unified memory this is plenty for Qwen3-32B at FP16.
- `--ipc=host` is required by vLLM for shared-memory between workers.

Detach from tmux with `Ctrl-b d`; reattach with `tmux attach -t vllm`.

The first launch takes a minute or two while weights are loaded. When you see:

```
INFO:     Uvicorn running on http://0.0.0.0:8000
```

it's ready.

#### 4. Open the firewall (if active)

DGX OS doesn't enable `ufw` by default, but if it's on:

```bash
sudo ufw allow 8000/tcp
```

#### 5. Verify locally on the DGX

```bash
curl -s http://localhost:8000/v1/models | jq
# Should list "Qwen/Qwen3-32B"
```

### From the Mac

Test connectivity:

```bash
curl -s http://192.168.191.4:8000/v1/models | jq
```

Then run `ask` against the remote backend:

```bash
go run ./cmd/ask \
  -chat-api openai \
  -chat-url http://192.168.191.4:8000 \
  -chat-model Qwen/Qwen3-32B
```

Or via Make (forwarded variables):

```bash
make ask CHAT_API=openai CHAT_URL=http://192.168.191.4:8000 CHAT_MODEL=Qwen/Qwen3-32B
```

The embedding side is unchanged — `nomic-embed-text` keeps running on the Mac's Ollama.

### Setting it as your default

If you want the remote backend to be your daily driver, export the values in your shell profile (`~/.zshrc`):

```bash
export EMAILS_RAG_CHAT_API=openai
export EMAILS_RAG_CHAT_URL=http://192.168.191.4:8000
export EMAILS_RAG_CHAT_MODEL=Qwen/Qwen3-32B

# Then either alias or pass them to make:
alias ask='go run ~/stenator/emails-rag/cmd/ask \
  -chat-api $EMAILS_RAG_CHAT_API \
  -chat-url $EMAILS_RAG_CHAT_URL \
  -chat-model $EMAILS_RAG_CHAT_MODEL'
```

### Troubleshooting the remote setup

**`connection refused` from the Mac.**
The vLLM container is listening on `127.0.0.1` instead of `0.0.0.0`. Verify the launch command includes `--host 0.0.0.0`, and that the Docker `-p 8000:8000` port mapping is present. Then `nc -zv 192.168.191.4 8000` from the Mac to confirm the port is reachable on the LAN.

**`openai chat: status 404`.**
The base URL probably has `/v1` already on it. Don't include it — the client appends `/v1/chat/completions` itself. Pass `http://192.168.191.4:8000`, not `http://192.168.191.4:8000/v1`.

**`openai chat: status 400` with `model not found`.**
The `-chat-model` value must match exactly what vLLM serves at `/v1/models`. Run `curl http://192.168.191.4:8000/v1/models` to see the canonical name.

**Streaming feels choppy on the LAN.**
That's usually network MTU or the chat model itself being slow at first token. Run `curl http://192.168.191.4:8000/v1/models` to confirm latency is sub-ms; if so, the choppiness is generation-side.

**OOM on the DGX during model load.**
Drop `--gpu-memory-utilization` to `0.85`, or lower `--max-model-len`. If still tight, fall back to an AWQ-quantized variant (`Qwen/Qwen2.5-32B-Instruct-AWQ` is reliable).

---

## How it works

### Pipeline

```
PDFs  ──► pdftotext  ──┐
  (─► ocrmypdf fallback│
       when scanned)   │
                       ├─► chunk.Split ──► embed.Embed ──► store.InsertBatch
MBOX  ──► RFC 5322  ───┘                                    (SQLite, float32 BLOB)
          + MIME walk
          + QP/base64

question ──► embed.Embed ──► store.Search ──► llm.Chat (stream) ──► tokens
                             (cosine top-k)                          + sources
```

### Files (what to look at when something breaks)

| Path | Purpose |
|---|---|
| `cmd/index/main.go` | Indexer CLI. Parses flags, walks paths, drives the pipeline. |
| `cmd/ask/main.go` | REPL CLI. Reads questions, prints streamed answers, renders Sources. |
| `internal/extract/pdf.go` | Shell out to `pdftotext`; OCR fallback via `ocrmypdf`. |
| `internal/extract/mbox.go` | Stream mbox files, parse RFC 5322 + MIME, decode bodies. |
| `internal/chunk/chunk.go` | Paragraph-aware overlapping splitter. |
| `internal/embed/ollama.go` | HTTP client for Ollama `/api/embed`. |
| `internal/llm/ollama.go` | HTTP client for Ollama `/api/chat` (NDJSON streaming). |
| `internal/store/sqlite.go` | SQLite schema, vec encode/decode, brute-force cosine search. |
| `internal/pipeline/index.go` | Orchestrates extract → chunk → embed → store. |
| `internal/pipeline/search.go` | Orchestrates embed → search → chat; parses inline `source:` filter. |

### Design choices worth remembering

- **SQLite + brute-force cosine.** Simple, zero extra deps, fast enough for the target scale. The `Store.Search` signature is the documented swap point if/when the corpus outgrows ~100k chunks; replace the implementation with [sqlite-vec](https://github.com/asg017/sqlite-vec) and the rest of the code doesn't change.
- **Shell out for PDF text.** `pdftotext` produces better layout-aware text than any pure-Go PDF library, at the cost of a system dependency.
- **OCR is auto-detected, not flagged.** If text extraction returns less than 100 visible characters, OCR runs. If `ocrmypdf` is missing or fails, the original sparse text is kept (no hard failure).
- **Streaming is built into the LLM client.** The Ollama `/api/chat` endpoint is consumed as NDJSON; the client invokes a callback per token while assembling the full string.
- **PDFs dedup by source path; MBOX deduplicates per message.** Each email's `Message-ID` (or a content fingerprint when missing) is stored alongside its chunks; re-indexing a growing mbox only ingests the new mail. PDFs use the simpler path-based skip; mutate one and you need to delete its rows before re-indexing.

---

## Operational notes

### Data location

By default everything lives under `./data/`:

```text
data/
  index.db          (SQLite database)
  index.db-wal      (write-ahead log; created in WAL journal mode)
  index.db-shm      (shared-memory file)
```

These three files are the entire database. Back them up together, ideally after the indexer has fully exited (so the WAL is checkpointed).

### Resetting the index

If you want to start over from scratch:

```bash
rm -rf data/
# Or: make reset    (prompts before deleting)
```

The next `cmd/index` run will recreate the directory, the database, and the schema. There is no destructive `migrate down` — deletion is the way.

To selectively remove one source instead:

```bash
sqlite3 data/index.db \
  "DELETE FROM chunks WHERE source = '/absolute/path/to/file.pdf';"
```

The next index run on that path will then re-ingest it.

### Performance expectations

On an M4 / 32 GB:

- Embedding throughput with `nomic-embed-text`: roughly **500–1000 chunks/min** in batches of 16.
- Search latency at 10k chunks: **sub-100 ms** end-to-end (load → cosine → sort → top-k).
- Search latency at 100k chunks: typically **a few hundred ms**; still interactive.
- LLM first-token latency with `qwen3:30b-a3b`: **a few seconds**; generation speed ~30 tok/sec.

When the index passes ~100k chunks the brute-force scan stops being instant. That's the point at which the `sqlite-vec` swap pays off.

### Backup and portability

The database is a single SQLite file. Copying `data/index.db` (plus the `-wal` and `-shm` sidecars if present) to another Apple Silicon Mac with the same Ollama models pulled is a complete migration. No re-indexing required.

### Sensitivity

The defaults in `.gitignore` exclude `*.pdf`, `*.mbox`, and `data/` precisely because those files often contain personal data. If you do want to commit a sample PDF for tests, force-add it:

```bash
git add -f testdata/sample.pdf
```

---

## Testing

```bash
go test ./...
# Or: make test
```

Covers chunking boundaries, vector encode/decode, cosine math, source-filter parsing, MIME extraction (plain, multipart/alternative, HTML fallback, quoted-printable, base64, RFC 2047 headers, attachment skipping), and mbox file splitting. No Ollama required for tests — they're all pure-logic unit tests.

To run a single package's tests with verbose output:

```bash
go test -v ./internal/extract
```

To check the build without running tests:

```bash
go build ./...
go vet ./...
# Or: make build vet
```

---

## Troubleshooting

**`ollama embed: status 500` / connection refused.**
Ollama isn't running, or the embed model isn't pulled. Check:

```bash
curl -s http://localhost:11434/api/version
ollama list                          # is nomic-embed-text (or your model) listed?
ollama pull nomic-embed-text
```

**`pdftotext: command not found`** during indexing.
poppler isn't installed (or not on `PATH`). `brew install poppler` and reopen the terminal.

**Indexing a scanned PDF hangs for minutes.**
That's `ocrmypdf` running on each page. Large scanned documents legitimately take a while. If you'd rather skip OCR for now, you can temporarily uninstall `ocrmypdf` or move it off `PATH` — the indexer will detect failure and keep the sparse text. (No flag yet to disable OCR; see Extending.)

**`go: cannot find module providing package github.com/mattn/go-sqlite3`.**
You skipped `go mod tidy`. Run it from the project root.

**`Sources:` block shows the same chunk twice with similar scores.**
Expected when neighboring chunks both match — the overlap is doing its job. If it's a problem, lower `-k`.

**Answer says "the context does not contain enough information" even though you know it does.**
First check the Sources block — if the right document isn't in the top-k, the retrieval is the problem (try `-k 10`, or rephrase). If the right chunk *is* there, the chat model just isn't drawing the inference — try `qwen3:30b-a3b` if you're on a smaller model.

**Streaming output looks fine but no newlines.**
Ollama returns tokens including their internal whitespace. The terminal will get newlines wherever the model emits them. If you're piping to a file, line buffering may delay output.

**`ollama embed: got N embeddings for M inputs` error.**
The embedding model returned the wrong shape. Almost always means you've passed an empty string to the embedder. Empty inputs are filtered upstream by the chunker, but if you see this, file a bug — there's a regression.

**Switched embedding models, now retrieval is garbage.**
You can't mix embedding models within a single index. Either revert to the original model or wipe `data/` and re-index.

---

## Pushing to GitHub

The directory is not yet a git repo. The full first-push workflow:

### 1. Initialize the local repo

```bash
cd ~/stenator/emails-rag      # or wherever you cloned it
git init
git add .
git status                    # confirm .gitignore is excluding data/, *.mbox, *.pdf
git commit -m "Initial commit"
```

The `.gitignore` in this repo already excludes:

- `data/` (the SQLite index)
- `*.db`, `*.mbox`, `*.pdf` (binaries and user data)
- `.DS_Store`
- `/emails-rag`, `/ask`, `/index` (compiled binaries)
- Editor scratch files

### 2. Create the GitHub repo

In a browser at <https://github.com/new>:

- Owner: your account
- Name: `emails-rag`
- Visibility: **private** is the sensible default for a tool that touches your personal mail
- **Do not** initialize with a README, .gitignore, or license — you already have those locally

GitHub will show you the "push an existing repository" snippet. Use it:

```bash
git remote add origin git@github.com:<your-user>/emails-rag.git
git branch -M main
git push -u origin main
```

If you're using HTTPS instead of SSH:

```bash
git remote add origin https://github.com/<your-user>/emails-rag.git
```

### 3. Subsequent pushes

Standard flow:

```bash
git status
git add <files>
git commit -m "describe the change"
git push
```

Always check `git status` before committing — it's easy to accidentally `git add .` and pull in a stray PDF.

### 4. Cloning to a new machine

```bash
git clone git@github.com:<your-user>/emails-rag.git
cd emails-rag
# Then run the installation steps from the top of this README.
```

You'll still need to install poppler, ocrmypdf, Ollama, and pull the models on the new machine. The repo is just the code.

---

## Extending

Likely next moves, in rough order of value-per-effort:

- **Hybrid BM25 + vector search.** SQLite ships FTS5, so no new dependency. Run both rankers, fuse with reciprocal-rank, take top-k. Real quality win on queries that hinge on rare keywords (proper nouns, account numbers, error codes) where pure semantic search wanders.
- **Reranking.** After cosine top-k, run a small cross-encoder before passing to the chat model. Bigger quality bump for queries where the top-k contains the right chunks but in the wrong order.
- **Per-source-path filter.** If you want to ask only against, say, `~/Documents/legal`, the current `-source pdf` flag is too coarse. Add a substring/glob filter on `source`.
- **`sqlite-vec` swap.** When the corpus crosses ~100k chunks and brute-force scans get sluggish, replace `internal/store/sqlite.go` with a `sqlite-vec`-backed implementation. The public `Store.Search` signature is the contract; keep it stable and nothing else changes.
- **Pluggable LLM backends.** The `llm.Client` is small and HTTP-shaped. A sibling `internal/llm/openai.go` would let you switch backends with a flag.
- **Web UI.** A tiny local HTTP server with a single-page chat interface — stdlib `net/http` is enough. Lets you query from a browser without leaving the terminal.

---

## Project layout

```
.
├── .gitignore
├── Makefile                   (setup, models, build, test, ask, reset)
├── README.md                  (this file)
├── go.mod
├── go.sum
├── cmd/
│   ├── index/main.go          bulk indexer CLI
│   └── ask/main.go            interactive query CLI
└── internal/
    ├── extract/
    │   ├── pdf.go             pdftotext + ocrmypdf fallback
    │   ├── mbox.go            RFC 5322 + full MIME walk (captures Message-ID)
    │   └── mbox_test.go
    ├── chunk/
    │   ├── chunk.go           paragraph-aware overlapping splitter
    │   └── chunk_test.go
    ├── embed/ollama.go        ollama embeddings client
    ├── llm/
    │   ├── llm.go             Client interface + backend factory
    │   ├── ollama.go          Ollama NDJSON streaming chat client
    │   ├── openai.go          OpenAI-compatible (vLLM, LM Studio) SSE chat client
    │   └── openai_test.go
    ├── store/
    │   ├── sqlite.go          schema + float32 BLOB vector store
    │   └── sqlite_test.go
    └── pipeline/
        ├── index.go           extract → chunk → embed → store (per-message dedup)
        ├── index_test.go
        ├── search.go          embed → cosine → chat (streaming)
        └── search_test.go
```
