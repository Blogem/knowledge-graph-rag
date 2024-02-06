# Introduction

This is far from complete. It's just some notes and partially implemented application to apply the RAG pattern with Neo4j as data source.

# Install

1. Install [Ollama](https://ollama.ai/download).
2. Clone this repo: https://github.com/docker/genai-stack
3. Follow their instructions:
   1. `docker compose up`
   2. `docker compose up --build`
   3. `docker compose watch`
   4. `docker compose down`
4. Find out that half of it is broken.

-----

1. start Ollama
2. Setup Python app for embeddings (FastAPI, uvicorn ...)
2. set env: `source .env`
3. download latest data dump: https://github.com/neo4j-graph-examples/recommendations/tree/main/data and put file named `neo4j.dump` in /backups dir of neo4j. The file name maps to the database name (only neo4j database in community edition).
3. load data
```
docker run --interactive --tty --rm \
    --volume=$HOME/code/explore/knowledge-graph-rag/neo4j/data:/data \
    --volume=$HOME/code/explore/knowledge-graph-rag/neo4j/backups:/backups \
    neo4j/neo4j-admin:5.16.0 \
neo4j-admin database load neo4j --from-path=/backups
```
4. start neo4j:
```
docker run \
    --user="$(id -u)":"$(id -g)" \
    --restart always -d \
    -v $HOME/code/explore/knowledge-graph-rag/neo4j/data:/data \
    -v $HOME/code/explore/knowledge-graph-rag/neo4j/backups:/backups \
    -v $HOME/code/explore/knowledge-graph-rag/neo4j/logs:/logs \
    -v $HOME/code/explore/knowledge-graph-rag/neo4j/import:/var/lib/neo4j/import \
    --publish=7474:7474 --publish=7687:7687 \
    --env NEO4J_AUTH=$NEO4J_AUTH \
     --env NEO4J_PLUGINS='["apoc"]' \
    neo4j:5.16.0
```
5. create index (move to code later)
```
CALL db.index.vector.createNodeIndex(
    'moviePlots',
    'Movie',
    'embedding',
    4096,
    'cosine'
)
```
6. Generate embeddings (once):
   1. start Python app for embeddings endpoint (not the right embeddings from Ollama, not the right model supported)
   2. run Go app with `-embeddings` flag to fetch movies for neo4j, ask for embeddings, insert embeddings into neo4j
7. Call Go app with `-prompt` flag followed by key words or description of a movie you want to watch.

# TODO:

- Think of prompt that uses the relationships in the graph as useful information (instead of only relying on similarity search).
- Create app to ask a question with the knowledge graph as augmentation data source.
- Create app to create and load a knowledge graph based on a text sources that describes relationships.

---

- Prompt used in genai-stack:
```
general_system_template = """ 
Use the following pieces of context to answer the question at the end.
The context contains question-answer pairs and their links from Stackoverflow.
You should prefer information from accepted or more upvoted answers.
Make sure to rely on information from the answers and not on questions to provide accurate responses.
When you find particular answer in the context useful, make sure to cite it in the answer using the link.
If you don't know the answer, just say that you don't know, don't try to make up an answer.
----
{summaries}
----
Each answer you generate should contain a section at the end of links to 
Stackoverflow questions and answers you found useful, which are described under Source value.
You can only use links to StackOverflow questions that are present in the context and always
add links to the end of the answer in the style of citations.
Generate concise answers with references sources section of links to 
relevant StackOverflow questions only at the end of the answer.
"""
```

- Query to neo4j vector in genai-stack:
```
# Vector + Knowledge Graph response
kg = Neo4jVector.from_existing_index(
    embedding=embeddings,
    url=embeddings_store_url,
    username=username,
    password=password,
    database="neo4j",  # neo4j by default
    index_name="stackoverflow",  # vector by default
    text_node_property="body",  # text by default
    retrieval_query="""
WITH node AS question, score AS similarity
CALL  { with question
    MATCH (question)<-[:ANSWERS]-(answer)
    WITH answer
    ORDER BY answer.is_accepted DESC, answer.score DESC
    WITH collect(answer)[..2] as answers
    RETURN reduce(str='', answer IN answers | str + 
            '\n### Answer (Accepted: '+ answer.is_accepted +
            ' Score: ' + answer.score+ '): '+  answer.body + '\n') as answerTexts
} 
RETURN '##Question: ' + question.title + '\n' + question.body + '\n' 
    + answerTexts AS text, similarity as score, {source: question.link} AS metadata
ORDER BY similarity ASC // so that best answers are the last
""",
)
```