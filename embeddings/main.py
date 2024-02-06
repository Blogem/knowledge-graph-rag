from sentence_transformers import SentenceTransformer
import csv

from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI()


def main():
    # get all the plots from csv file with format: movie_id, plot
    # generate embeddings for each plot
    # write embeddings to a new csv file with format: movie_id, embedding
    model = 'sentence-transformers/all-MiniLM-L6-v2'
    data = load_data()
    print(data)
    embeddings = generate_embeddings(model, data)
    write_embeddings(embeddings)


def load_data() -> dict[str, str]:
    # load data from csv file
    # return data
    file = 'data/plots.csv'
    data = {}
    with open(file, 'r') as f:
        reader = csv.DictReader(f)
        for row in reader:
            data[row['movie_id']] = row['plot']
    return data


def generate_embeddings(model: str, data: dict[str, str]) -> dict[str, list[float]]:
    embeddings = {}
    for movie_id, plot in data.items():
        embedding = generate_embedding(model, plot)
        embeddings[movie_id] = embedding
    return embeddings


def generate_embedding(model, prompt: str) -> list[float]:
    model = SentenceTransformer(model)
    return model.encode([prompt])[0]


def write_embeddings(embeddings: dict[str, list[float]]):
    with open('../neo4j/import/embeddings.csv', 'w') as f:
        writer = csv.writer(f)
        writer.writerow(['movie_id', 'embedding'])
        for movie_id, embedding in embeddings.items():
            writer.writerow([movie_id, '['+','.join(map(str, embedding))+']'])


@app.get("/")
def read_root():
    return {"Hello": "World"}


class EmbeddingRequest(BaseModel):
    model: str
    prompt: str


# curl -X 'POST' \
#   'http://127.0.0.1:8000/api/embeddings' \
#   -H 'Content-Type: application/json' \
#   -d '{
#   "model": "sentence-transformers/all-MiniLM-L6-v2",
#   "prompt": "string"
# }'
@app.post("/api/embeddings")
def read_embedding(r: EmbeddingRequest) -> dict[str, list[float]]:
    # model = 'sentence-transformers/all-MiniLM-L6-v2'
    return {'embedding': generate_embedding(r.model, r.prompt)}


# if __name__ == '__main__':
    # main()
