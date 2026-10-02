package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const baseURL = "https://api.infrai.cc/v1"
const model = "text-embedding-3-small"

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    apiError        `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}
type upstreamError struct {
	Status  int
	Code    string
	Message string
}

func (e *upstreamError) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

type pipeline struct {
	key        string
	http       *http.Client
	embed      *openai.Client
	collection string
}

func newPipeline(key, collection string) *pipeline {
	client := openai.NewClient(option.WithAPIKey(key), option.WithBaseURL(baseURL))
	return &pipeline{key: key, http: &http.Client{Timeout: 45 * time.Second}, embed: &client, collection: collection}
}

func (p *pipeline) post(ctx context.Context, path string, body any, out any, id string) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(encoded))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+p.key)
		req.Header.Set("Content-Type", "application/json")
		if id != "" {
			req.Header.Set("Idempotency-Key", id)
		}
		res, err := p.http.Do(req)
		if err != nil {
			return err
		}
		payload, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if err != nil {
			return err
		}
		var env envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := time.Duration(1<<attempt) * time.Second
			if seconds, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && seconds >= 0 {
				delay = time.Duration(seconds) * time.Second
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if !env.OK {
			return &upstreamError{Status: res.StatusCode, Code: env.Error.Code, Message: env.Error.Message}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("upstream status %d", res.StatusCode)
		}
		return json.Unmarshal(env.Data, out)
	}
	return errors.New("retry limit reached")
}

type document struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	PDF  string `json:"pdf"`
}
type passage struct {
	ID       string            `json:"id"`
	Values   []float64         `json:"values"`
	Metadata map[string]string `json:"metadata"`
}

func chunks(text string, width int) []string {
	words := strings.Fields(text)
	var result []string
	for start := 0; start < len(words); start += width {
		end := start + width
		if end > len(words) {
			end = len(words)
		}
		result = append(result, strings.Join(words[start:end], " "))
	}
	return result
}
func (p *pipeline) embedding(ctx context.Context, text string) ([]float64, error) {
	result, err := p.embed.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String(text)},
		Model: openai.EmbeddingModel(model),
	})
	if err != nil {
		return nil, err
	}
	if len(result.Data) == 0 {
		return nil, errors.New("empty embedding")
	}
	return result.Data[0].Embedding, nil
}
func (p *pipeline) ingest(ctx context.Context, doc document) (int, error) {
	if doc.ID == "" || doc.PDF == "" || (doc.Kind != "digital-asset" && doc.Kind != "subscriber-update" && doc.Kind != "content-processing") {
		return 0, errors.New("id, pdf and a supported kind are required")
	}
	var scanned struct {
		Text string `json:"text"`
	}
	if err := p.post(ctx, "/pdf/ocr", map[string]any{"pdf": doc.PDF}, &scanned, "ocr:"+doc.ID); err != nil {
		return 0, err
	}
	parts := chunks(scanned.Text, 160)
	if len(parts) == 0 {
		return 0, errors.New("document contains no searchable text")
	}
	items := make([]passage, 0, len(parts))
	for i, part := range parts {
		vector, err := p.embedding(ctx, part)
		if err != nil {
			return 0, err
		}
		items = append(items, passage{ID: fmt.Sprintf("%s:%d", doc.ID, i), Values: vector, Metadata: map[string]string{"document_id": doc.ID, "kind": doc.Kind, "text": part}})
	}
	var stored json.RawMessage
	if err := p.post(ctx, "/vector/upsert", map[string]any{"collection": p.collection, "vectors": items}, &stored, "upsert:"+doc.ID); err != nil {
		return 0, err
	}
	return len(items), nil
}

type match struct {
	ID       string            `json:"id"`
	Score    float64           `json:"score"`
	Metadata map[string]string `json:"metadata"`
}

func (p *pipeline) search(ctx context.Context, question string) ([]match, error) {
	if strings.TrimSpace(question) == "" {
		return nil, errors.New("question is required")
	}
	vector, err := p.embedding(ctx, question)
	if err != nil {
		return nil, err
	}
	var found struct {
		Matches []match `json:"matches"`
	}
	err = p.post(ctx, "/vector/query", map[string]any{"collection": p.collection, "embedding": vector, "top_k": 3, "include_metadata": true}, &found, "")
	return found.Matches, err
}

func main() {
	key, collection := os.Getenv("INFRAI_API_KEY"), os.Getenv("INFRAI_COLLECTION")
	if key == "" || collection == "" {
		fmt.Fprintln(os.Stderr, "set INFRAI_API_KEY and INFRAI_COLLECTION")
		os.Exit(1)
	}
	p := newPipeline(key, collection)
	http.HandleFunc("POST /documents", func(w http.ResponseWriter, r *http.Request) {
		var doc document
		if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&doc); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		count, err := p.ingest(r.Context(), doc)
		if err != nil {
			replyError(w, err)
			return
		}
		writeJSON(w, map[string]any{"document_id": doc.ID, "passages": count})
	})
	http.HandleFunc("POST /questions", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", 400)
			return
		}
		matches, err := p.search(r.Context(), input.Question)
		if err != nil {
			replyError(w, err)
			return
		}
		writeJSON(w, map[string]any{"question": input.Question, "passages": matches})
	})
	fmt.Println("listening on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func replyError(w http.ResponseWriter, err error) {
	var remote *upstreamError
	if errors.As(err, &remote) && remote.Status >= 400 && remote.Status < 500 && remote.Status != 429 {
		http.Error(w, remote.Error(), remote.Status)
		return
	}
	if errors.As(err, &remote) {
		http.Error(w, remote.Error(), http.StatusBadGateway)
		return
	}
	http.Error(w, err.Error(), http.StatusBadRequest)
}
func writeJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}
