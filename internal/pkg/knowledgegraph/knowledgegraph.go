package knowledgegraph

import (
	"context"
	"fmt"
	"log"

	"github.com/blogem/knowledge-graph-rag/internal/pkg/embeddings"
	"github.com/blogem/knowledge-graph-rag/internal/pkg/utils"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

type KnowledgeGraph interface {
	HelloWorld(ctx context.Context, uri, username, password string) (string, error)
	StoreEmbeddings(ctx context.Context, embbedingsFile string) error
	GetMovies(ctx context.Context) ([]Movie, error)
	SearchSimilarPlots(ctx context.Context, plot string) ([]Movie, error)
}

type knowledgeGraph struct {
	session  neo4j.SessionWithContext
	embedder *embeddings.Service
}

func NewKnowledgeGraph(ctx context.Context, uri string, username string, password string, embedder *embeddings.Service) (KnowledgeGraph, error) {
	var g knowledgeGraph
	err := g.startSession(ctx, uri, username, password)
	if err != nil {
		return nil, err
	}
	g.embedder = embedder
	return &g, nil
}

func (g *knowledgeGraph) startSession(ctx context.Context, uri, username, password string) error {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return err
	}
	g.session = driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	return nil
}

func (g *knowledgeGraph) closeSession(ctx context.Context) {
	g.session.Close(ctx)

}

func (g *knowledgeGraph) StoreEmbeddings(ctx context.Context, embbedingsFile string) error {
	// LOAD CSV WITH HEADERS
	// FROM 'file:///embeddings.csv'
	// AS row
	// MATCH (m:Movie {movieId: row.movie_id})
	// CALL db.create.setNodeVectorProperty(m, 'embedding', apoc.convert.fromJsonList(row.embedding))
	// RETURN count(*)
	query := fmt.Sprintf(`
		LOAD CSV WITH HEADERS
		FROM 'file:///%s'
		AS row
		MATCH (m:Movie {movieId: row.movie_id})
		CALL db.create.setNodeVectorProperty(m, 'embedding', apoc.convert.fromJsonList(row.embedding))
		RETURN count(*)
	`, embbedingsFile)
	result, err := g.session.Run(ctx, query, nil)
	if err != nil {
		return err
	}
	if result.Err() != nil {
		return result.Err()
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("properties set: %+v\n", summary.Counters().PropertiesSet()) // not sure why this is always returning 0. It's definitely adding properties
	return nil
}

type Movie struct {
	Languages       []any   `json:"languages"`
	Year            int64   `json:"year"`
	ImdbID          string  `json:"imdbId"`
	Runtime         int64   `json:"runtime"`
	ImdbRating      float64 `json:"imdbRating"`
	MovieID         string  `json:"movieId"`
	Countries       []any   `json:"countries"`
	ImdbVotes       int64   `json:"imdbVotes"`
	Title           string  `json:"title"`
	URL             string  `json:"url"`
	Revenue         int64   `json:"revenue"`
	TmdbID          string  `json:"tmdbId"`
	Plot            string  `json:"plot"`
	Poster          string  `json:"poster"`
	Released        string  `json:"released"`
	Budget          int64   `json:"budget"`
	SimilarityScore float64 `json:"similarityScore"`
}

func (g *knowledgeGraph) GetMovies(ctx context.Context) ([]Movie, error) {
	query := `
		MATCH (m:Movie)
		WHERE m.movieId IS NOT NULL
		AND m.plot IS NOT NULL
		RETURN m.movieId, m.plot
	`
	movies, err := g.getMoviePlots(ctx, query)
	if err != nil {
		return nil, err
	}
	return movies, nil
}

// getMoviePlots retrieves movie plots from the knowledge graph.
func (g *knowledgeGraph) getMoviePlots(ctx context.Context, query string) ([]Movie, error) {
	result, err := g.session.Run(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var movies []Movie
	for result.Next(ctx) {
		record := result.Record()

		movieId, ok := record.Get("m.movieId")
		if !ok {
			fmt.Println("movieId not found")
			continue
		}

		plot, ok := record.Get("m.plot")
		if !ok {
			fmt.Println("plot not found")
			continue
		}

		movie := Movie{
			MovieID: movieId.(string),
			Plot:    plot.(string),
		}
		movies = append(movies, movie)
	}

	return movies, result.Err()
}

func (g *knowledgeGraph) SearchSimilarPlots(ctx context.Context, plot string) ([]Movie, error) {
	embedding, err := g.embedder.Embedding(ctx, plot)
	if err != nil {
		return nil, err
	}

	query := fmt.Sprintf(`
	MATCH (m:Movie)

	CALL db.index.vector.queryNodes('moviePlots', 6, [%s])
	YIELD node, score
	
	RETURN node.title AS title, node.plot AS plot, score
	LIMIT 6
	`, utils.Float32SliceToString(embedding.Embedding))

	log.Println(query)

	result, err := g.session.Run(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var movies []Movie
	for result.Next(ctx) {
		record := result.Record()

		title, ok := record.Get("title")
		if !ok {
			fmt.Println("title not found")
			continue
		}
		plot, ok := record.Get("plot")
		if !ok {
			fmt.Println("plot not found")
			continue
		}
		score, ok := record.Get("score")
		if !ok {
			fmt.Println("score not found")
			continue
		}
		_ = score

		movie := Movie{
			Title:           title.(string),
			Plot:            plot.(string),
			SimilarityScore: score.(float64),
		}
		movies = append(movies, movie)
	}

	return movies, result.Err()
}

func (g *knowledgeGraph) HelloWorld(ctx context.Context, uri, username, password string) (string, error) {
	driver, err := neo4j.NewDriverWithContext(uri, neo4j.BasicAuth(username, password, ""))
	if err != nil {
		return "", err
	}
	defer driver.Close(ctx)

	session := driver.NewSession(ctx, neo4j.SessionConfig{AccessMode: neo4j.AccessModeWrite})
	defer session.Close(ctx)

	greeting, err := session.ExecuteWrite(ctx, func(transaction neo4j.ManagedTransaction) (any, error) {
		result, err := transaction.Run(ctx,
			"CREATE (a:Greeting) SET a.message = $message RETURN a.message + ', from node ' + id(a)",
			map[string]any{"message": "hello, world"})
		if err != nil {
			return nil, err
		}

		if result.Next(ctx) {
			return result.Record().Values[0], nil
		}

		return nil, result.Err()
	})
	if err != nil {
		return "", err
	}

	return greeting.(string), nil
}
