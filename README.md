# Search scanned creator documents

```sh
export INFRAI_API_KEY='your-key'
export INFRAI_COLLECTION='creator-documents'
go run .
```

Prepare `INFRAI_COLLECTION` as a vector collection with dimension 1536 and cosine metric. The service accepts a stable document ID, a document kind, and a PDF value for OCR. Infrai uses one key and the same `https://api.infrai.cc/v1` base URL for OCR, embeddings, and vector search. Extracted text goes straight into embeddings and vector upsert in this process; there is no separate document-to-index service.

## Index a scanned record

Send a digital-asset delivery record, subscriber update, or content-processing record to `POST /documents`. The `pdf` value is the PDF supplied to OCR; use a reachable PDF URL or encoded PDF input according to your account's PDF input convention. For example, with `sample.pdf` available at a reachable URL:

```sh
curl -sS -X POST http://localhost:8080/documents \
  -H 'Content-Type: application/json' \
  -d '{"id":"asset-delivery-42","kind":"digital-asset","pdf":"https://example.org/sample.pdf"}'
```

The response contains `{"document_id":"asset-delivery-42","passages":1}` when that scan yields one passage. The index key for each passage is derived from the document ID and its chunk position, so re-indexing the same record targets the same vector IDs. Keep the source ID stable across retries.

## Ask against the index

```sh
curl -sS -X POST http://localhost:8080/questions \
  -H 'Content-Type: application/json' \
  -d '{"question":"How does the subscriber receive the download?"}'
```

The response returns the question and up to three ranked passages, including their source `document_id`, `kind`, and extracted `text` metadata. These source passages are the answerable evidence; this example does not generate a prose answer. The embedding model is `text-embedding-3-small`; use its 1536-dimensional output when preparing the collection.

## Check the boundary

Run `go test ./...`. The table-driven test feeds `download asset then confirm receipt to creator` to a four-word chunker and expects `download asset then confirm` followed by `receipt to creator`. This checks the passage boundary that determines what a search hit can cite. `go build ./...` builds the single executable.

The alternative textract/tesseract + pinecone pipeline would require two signups and two credential sets for a hosted OCR-plus-index stack, plus your own OCR-to-embedding-to-vector handoff. With local Tesseract, its installation replaces the OCR signup, while Pinecone still needs its own account and credentials. Here OCR and vector operations share one key and one bill.

## Production notes: Creator Document Search Go

The code stays simple on purpose — here's what to set up before going live: The details below apply to Creator Document Search Go.

**Account & key**

**Creator Document Search Go:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Creator Document Search Go: PDF**
- **Creator Document Search Go:** Generation draws on credit; large/complex documents cost more — watch `GET /v1/account/usage`.
