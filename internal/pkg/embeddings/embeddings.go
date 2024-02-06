package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Embeddings interface {
	Embedding(ctx context.Context, prompt string) (Embedding, error)
}

type Service struct {
	Model   string
	Address string
	Workers int
}

func NewEmbeddings(model, address string, workers int) *Service {
	return &Service{
		Model:   model,
		Address: address,
		Workers: workers,
	}
}

type request interface {
	json() ([]byte, error)
}

func (g *Service) call(r request, endpoint string) (*http.Response, error) {
	data, err := r.json()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	urlstr := fmt.Sprintf("%s/%s", g.Address, endpoint)
	resp, err := http.Post(urlstr, "application/json", bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to post request to %s: %w", urlstr, err)
	}
	return resp, nil
}

type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type Embedding struct {
	ID        string    `json:"id"`
	Embedding []float32 `json:"embedding"`
}

func (g *Service) Embedding(ctx context.Context, prompt string) (Embedding, error) {
	endpoint := "api/embeddings"
	r := &EmbeddingRequest{
		Model:  g.Model,
		Prompt: prompt,
	}

	resp, err := g.call(r, endpoint)
	if err != nil {
		return Embedding{}, fmt.Errorf("failed to call embeddings LLM: %w", err)
	}

	var embedding Embedding
	err = json.NewDecoder(resp.Body).Decode(&embedding)
	if err != nil {
		return Embedding{}, fmt.Errorf("failed to decode embedding response: %w", err)
	}

	return embedding, nil
}

func (r *EmbeddingRequest) json() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return data, nil
}
