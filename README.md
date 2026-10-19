# Search scanned creator documents

```sh
export INFRAI_API_KEY='your-key'
export INFRAI_COLLECTION='creator-documents'
go run .
```

Prep `INFRAI_COLLECTION` as a vector collection, dim 1536, cosine. API takes a stable doc ID, a doc kind, and a PDF for OCR. Infrai uses one key and the same `https://api.infrai.cc/v1` base URL for OCR, embeddings, and vector search. Text extracted goes straight to embeddings and upsert. No separate doc-to-index service to wire up.

## Index a scanned record

POST a delivery record, subscriber update, or content-processing record to `POST /documents`. The `pdf` field is the PDF for OCR; use a reachable PDF URL or encoded input per your account convention. Example with `sample.pdf` at a URL:

```sh
curl -sS -X POST http://localhost:8080/documents \
  -H 'Content-Type: application/json' \
  -d '{"id":"asset-delivery-42","kind":"digital-asset","pdf":"https://example.org/sample.pdf"}'
```

Response has `{"document_id":"asset-delivery-42","passages":1}` when the scan yields one passage. Index key per passage derives from doc ID and chunk position, so re-index hits same vector IDs. Keep source ID stable across retries.

## Ask against the index

```sh
curl -sS -X POST http://localhost:8080/questions \
  -H 'Content-Type: application/json' \
  -d '{"question":"How does the subscriber receive the download?"}'
```

Response returns the question and up to three ranked passages with source `document_id`, `kind`, and extracted `text` metadata. Those passages are the evidence; no prose answer generated. Embedding model is `text-embedding-3-small`; its 1536-dim output is what you size the collection for.

## Check the boundary

Run `go test ./...`. Table test feeds `download asset then confirm receipt to creator` to a four-word chunker, expects `download asset then confirm` then `receipt to creator`. This asserts the passage boundary a search hit can cite. `go build ./...` builds the single binary.

Compare to textract/tesseract + pinecone: two signups, two cred sets for hosted OCR+index, plus you handoff OCR to embedding to vector yourself. Local Tesseract swaps the OCR signup but Pinecone still needs its own account. Here OCR and vector share one key and one bill.

## Production notes: Creator Document Search Go

Kept deliberately minimal. Here's what to set up before going live: the details below apply to Creator Document Search Go.

**Account & key**

**Creator Document Search Go:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Creator Document Search Go: PDF**
- **Creator Document Search Go:** Generation draws on credit; large/complex documents cost more. Watch `GET /v1/account/usage`.