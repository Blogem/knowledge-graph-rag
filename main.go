package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blogem/knowledge-graph-rag/internal/pkg/embeddings"
	"github.com/blogem/knowledge-graph-rag/internal/pkg/knowledgegraph"
	"github.com/blogem/knowledge-graph-rag/internal/pkg/ollama"
	"github.com/blogem/knowledge-graph-rag/internal/pkg/utils"
)

type LLMResponse struct {
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

type Chunk struct {
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	Response  string    `json:"response"`
	Done      bool      `json:"done"`
}

type LLMRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

func setupLLM() ollama.LLM {
	model := os.Getenv("LLM_MODEL")
	if model == "" {
		fmt.Println("LLM_MODEL not set, using default model")
		model = "llama2"
	}
	host := os.Getenv("LLM_HOST")
	if host == "" {
		fmt.Println("LLM_HOST not set, using default host")
		host = "http://localhost:11434"
	}
	return ollama.NewOllama(model, host)
}

func setupKG(ctx context.Context, embedder *embeddings.Service) knowledgegraph.KnowledgeGraph {
	neo4jUri := os.Getenv("NEO4J_URI")
	if neo4jUri == "" {
		fmt.Println("NEO4J_URI not set, using default uri")
		neo4jUri = "bolt://localhost:7687"
	}
	neo4jUser := os.Getenv("NEO4J_USER")
	if neo4jUser == "" {
		log.Fatal("NEO4J_USER not set")
	}
	neo4jPassword := os.Getenv("NEO4J_PASSWORD")
	if neo4jPassword == "" {
		log.Fatal("NEO4J_PASSWORD not set")
	}
	kg, err := knowledgegraph.NewKnowledgeGraph(ctx, neo4jUri, neo4jUser, neo4jPassword, embedder)
	if err != nil {
		log.Fatal(err)
	}
	return kg
}

func setupEmbedder() *embeddings.Service {
	embeddingsModel := os.Getenv("EMBEDDINGS_MODEL")
	if embeddingsModel == "" {
		fmt.Println("EMBEDDINGS_MODEL not set, using default model")
		embeddingsModel = "sentence-transformers/all-MiniLM-L6-v2"
	}
	embeddingsHost := os.Getenv("EMBEDDINGS_HOST")
	if embeddingsHost == "" {
		fmt.Println("EMBEDDINGS_HOST not set, using default host")
		embeddingsHost = "http://localhost:8000"
	}
	embeddingsWorkersEnv := os.Getenv("EMBEDDINGS_WORKERS")
	var embeddingsWorkers int
	if embeddingsWorkersEnv == "" {
		if embeddingsWorkers < 1 {
			fmt.Println("EMBEDDINGS_WORKERS not set, using default workers")
			embeddingsWorkers = 4
		}
	} else {
		var err error
		embeddingsWorkers, err = strconv.Atoi(embeddingsWorkersEnv)
		if err != nil {
			log.Fatal(err)
		}
	}
	return embeddings.NewEmbeddings(embeddingsModel, embeddingsHost, embeddingsWorkers)
}

type ErrEmbedding struct {
	errors []error
}

func (e ErrEmbedding) Error() string {
	var msgs []string
	for _, err := range e.errors {
		msgs = append(msgs, err.Error())
	}
	return fmt.Sprintf("error fetching embeddings: %s", strings.Join(msgs, ", "))
}

func fetchEmbeddingsForMovies(ctx context.Context, embedder *embeddings.Service, movies []knowledgegraph.Movie, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString("movie_id,embedding\n")
	if err != nil {
		return err
	}

	resChan, errChan := createEmbeddingsWorkers(ctx, embedder, movies)

	var errors []error
	for {
		select {
		case embedding, ok := <-resChan:
			if !ok {
				if len(errors) > 0 {
					return ErrEmbedding{
						errors: errors,
					}
				}
				return nil
			}
			embeddingStr := utils.Float32SliceToString(embedding.Embedding)
			_, err := file.WriteString(fmt.Sprintf("%s,\"[%s]\"\n", embedding.ID, embeddingStr))
			if err != nil {
				errors = append(errors, err)
			}
		case err, ok := <-errChan:
			if ok {
				errors = append(errors, err)
			}
		default:
			continue
		}
	}
}

func createEmbeddingsWorkers(ctx context.Context, embedder *embeddings.Service, movies []knowledgegraph.Movie) (chan embeddings.Embedding, chan error) {
	jobChan := make(chan knowledgegraph.Movie)
	resChan := make(chan embeddings.Embedding)
	errChan := make(chan error)

	wg := &sync.WaitGroup{}
	for i := 0; i < embedder.Workers; i++ {
		log.Println("creating embeddings worker")
		wg.Add(1)
		go createEmbeddingsWorker(ctx, wg, embedder, jobChan, resChan, errChan)
	}

	go func() {
		wg.Wait()
		close(resChan)
		close(errChan)
	}()

	go func(ctx context.Context) {
		for _, movie := range movies {
			log.Printf("creating embedding for movie %s", movie.MovieID)
			select {
			case <-ctx.Done():
				log.Println("request canceled by client")
				close(jobChan)
				return
			default:
				jobChan <- movie
			}
		}
		close(jobChan)
	}(ctx)

	return resChan, errChan
}

func createEmbeddingsWorker(ctx context.Context, wg *sync.WaitGroup, embedder *embeddings.Service, jobChan chan knowledgegraph.Movie, resChan chan embeddings.Embedding, errChan chan error) {
	defer wg.Done()
	for {
		movie, ok := <-jobChan
		if !ok {
			return
		}
		embedding, err := embedder.Embedding(ctx, movie.Plot)
		if err != nil {
			errChan <- err
			continue
		}
		embedding.ID = movie.MovieID
		resChan <- embedding
	}
}

func parseFlags() (bool, string) {
	embeddingsFlag := flag.Bool("embeddings", false, "generate embeddings for movie plots in knowledge graph")
	promptFlag := flag.String("prompt", "", "prompt for language model")
	flag.Parse()
	if *promptFlag == "" && !*embeddingsFlag {
		log.Fatal("prompt flag or embeddings flag is required")
	}
	if *promptFlag != "" && *embeddingsFlag {
		log.Fatal("prompt and embeddings flags are mutually exclusive")
	}
	return *embeddingsFlag, *promptFlag
}

func main() {
	ctx := context.Background()
	generateEmbeddings, prompt := parseFlags()

	llm := setupLLM()
	embedder := setupEmbedder()
	kg := setupKG(ctx, embedder)

	movies, err := kg.GetMovies(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if generateEmbeddings {
		err := fetchEmbeddingsForMovies(ctx, embedder, movies, "neo4j/import/embeddings.csv")
		if err != nil {
			log.Fatal(err)
		}
		err = kg.StoreEmbeddings(ctx, "embeddings.csv")
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("embeddings generated and stored in knowledge graph")
		return
	}

	similarMovies, err := kg.SearchSimilarPlots(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}

	var moviesStr string
	for _, movie := range similarMovies {
		log.Println("movie:", movie.Title)
		moviesStr += fmt.Sprintf("Title: %s\nPlot: %s\n---\n", movie.Title, movie.Plot)
	}

	prompt = fmt.Sprintf(`
You are a movie expert. You decide which movie to watch based on the plot. Below are some movies with
plots based on the query of the user. You can only suggest movies from the list provided.

### Movies:
---
%s
Question: I want to watch a movie about %s. What movie from the list provided above should I watch?
You can only suggest movies from the list provided.
	`, moviesStr, prompt)

	log.Println("prompt created:\n", prompt)

	scanner, err := llm.GenerateStream(ctx, prompt)
	if err != nil {
		log.Fatal(err)
	}
	_, err = readStream(scanner, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
}

func readStream(scanner *bufio.Scanner, writer io.Writer) (*LLMResponse, error) {
	var response *LLMResponse
	var answer string
	for scanner.Scan() {
		var chunk Chunk
		err := json.Unmarshal(scanner.Bytes(), &chunk)
		if err != nil {
			log.Fatal(err)
		}
		_, err = writer.Write([]byte(chunk.Response))
		if err != nil {
			return nil, err
		}

		if chunk.Done {
			err := json.Unmarshal(scanner.Bytes(), &response)
			if err != nil {
				return nil, err
			}
			response.Response = answer
			break
		}
		answer += chunk.Response
	}
	_, err := writer.Write([]byte("\n"))
	if err != nil {
		return nil, err
	}

	return response, scanner.Err()
}

func retrieveAdditionalContext(ctx context.Context, kg knowledgegraph.KnowledgeGraph) ([]knowledgegraph.Movie, error) {
	// example in langchain:
	// kg = Neo4jVector.from_existing_index(
	//     embedding=embeddings,
	//     url=embeddings_store_url,
	//     username=username,
	//     password=password,
	//     database="neo4j",  # neo4j by default
	//     index_name="stackoverflow",  # vector by default
	//     text_node_property="body",  # text by default
	//     retrieval_query="""
	// WITH node AS question, score AS similarity
	// CALL  { with question
	//     MATCH (question)<-[:ANSWERS]-(answer)
	//     WITH answer
	//     ORDER BY answer.is_accepted DESC, answer.score DESC
	//     WITH collect(answer)[..2] as answers
	//     RETURN reduce(str='', answer IN answers | str +
	//             '\n### Answer (Accepted: '+ answer.is_accepted +
	//             ' Score: ' + answer.score+ '): '+  answer.body + '\n') as answerTexts
	// }
	// RETURN '##Question: ' + question.title + '\n' + question.body + '\n'
	//     + answerTexts AS text, similarity as score, {source: question.link} AS metadata
	// ORDER BY similarity ASC // so that best answers are the last
	// """,
	// )

	// connect with neo4j
	// query neo4j
	// return additional context
	// close connection

	// resp, err := kg.HelloWorld(context.Background(), "bolt://0.0.0.0:7687", "neo4j", "Tk2Y2XjMDze3hqNVBm")
	// "neo4j" , "stackoverflow", "body"

	return kg.SearchSimilarPlots(ctx, "movie where puppet comes to life")
}
