package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type LLM interface {
	Embedding(ctx context.Context, prompt string) (Embedding, error)
	Generate(ctx context.Context, prompt string) (string, error)
	GenerateStream(ctx context.Context, prompt string) (*bufio.Scanner, error)
}

type ollama struct {
	Model   string
	Address string
}

func NewOllama(model string, address string) LLM {
	return &ollama{
		Model:   model,   // "llama2",
		Address: address, // "http://localhost:11434",
	}
}

type request interface {
	json() ([]byte, error)
}

func (g *ollama) call(r request, endpoint string) (*http.Response, error) {
	data, err := r.json()
	if err != nil {
		return nil, err
	}
	// something seems broken with path.Join (or more likely: I'm using it wrong)
	// fmt.Println("g.Address", g.Address)
	// url, err := url.Parse(g.Address)
	// if err != nil {
	// 	return nil, errors.New("failed to parse url: " + err.Error())
	// }
	// fmt.Println("0", url)
	// fmt.Println("1", url.Path)
	// urlstr := path.Join(url.Path, endpoint)
	// fmt.Println("1.5", url)
	// fmt.Println("1.6", urlstr)
	// fmt.Println("2", url.Path)

	var urlstr string
	if g.Address[len(g.Address)-1] == '/' || endpoint[0] == '/' {
		urlstr = fmt.Sprintf("%s%s", g.Address, endpoint)
	} else {
		urlstr = fmt.Sprintf("%s/%s", g.Address, endpoint)
	}

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

func (g *ollama) Embedding(ctx context.Context, prompt string) (Embedding, error) {
	endpoint := "api/embeddings"
	r := &EmbeddingRequest{
		Model:  g.Model,
		Prompt: prompt,
	}

	resp, err := g.call(r, endpoint)
	if err != nil {
		return Embedding{}, fmt.Errorf("failed to call LLM: %w", err)
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

type GenerateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type GenerateResponse struct {
	Model              string    `json:"model"`
	CreatedAt          time.Time `json:"created_at"`
	Response           string    `json:"response"`
	Done               bool      `json:"done"`
	Context            []int     `json:"context"`
	TotalDuration      int64     `json:"total_duration"`
	LoadDuration       int       `json:"load_duration"`
	PromptEvalCount    int       `json:"prompt_eval_count"`
	PromptEvalDuration int64     `json:"prompt_eval_duration"`
	EvalCount          int       `json:"eval_count"`
	EvalDuration       int64     `json:"eval_duration"`
}

func (g *ollama) Generate(ctx context.Context, prompt string) (string, error) {
	endpoint := "/api/generate"
	r := &GenerateRequest{
		Model:  g.Model,
		Prompt: prompt,
		Stream: false,
	}

	resp, err := g.call(r, endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to call LLM: %w", err)
	}

	var gen GenerateResponse
	err = json.NewDecoder(resp.Body).Decode(&gen)
	if err != nil {
		return "", fmt.Errorf("failed to decode generate response: %w", err)
	}
	return gen.Response, nil
}

func (g *ollama) GenerateStream(ctx context.Context, prompt string) (*bufio.Scanner, error) {
	endpoint := "/api/generate"
	r := &GenerateRequest{
		Model:  g.Model,
		Prompt: prompt,
		Stream: true,
	}

	resp, err := g.call(r, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM: %w", err)
	}

	return bufio.NewScanner(resp.Body), nil
}

func (r *GenerateRequest) json() ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return data, nil
}
