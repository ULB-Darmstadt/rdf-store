# RDF store

> Store and search on RDF data that conform to [SHACL](https://www.w3.org/TR/shacl/).

## Installation
```
git clone https://gitlab.ulb.tu-darmstadt.de/rokit/rdf-store.git
cd rdf-store
docker compose up -d
```
Afterwards, open browser at http://localhost:8089

## Updating
```
cd rdf-store
git pull
docker compose up -d --build --force-recreate
```

## SHACL shapes (aka application profiles)
RDF store supports loading the SHACL shapes locally from the directory `rdf-store/backend/local/profiles/` or remotely from the [NFDI4Ing metadata profiles service](https://profiles.nfdi4ing.de). See the [.env](./.env) file on how to enable/disable/configure these sources.

## SPARQL endpoint
The stored RDF data can be queried with SPARQL at http://localhost:8089/api/v1/sparql/query

Example:
```
curl -Lk -X POST http://localhost:8089/api/v1/sparql/query -d "query=SELECT ?s ?p ?o WHERE { ?s ?p ?o }"
```

## Development setup
### Prerequisites
- install [go](https://go.dev/dl/)
- install [npm](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm)

When modifying the code in this repo (backend or frontend), the recommended steps for running/testing the local changes are:
1. `cd rdf-store`
1. edit [docker-compose.yml](./docker-compose.yml) and comment in the port mappings for fuseki and solr
1. `docker compose start solr fuseki`
1. `cd backend && go run .` -> starts backend on port 3000, needs to be run again after each change in the backend
1. `cd frontend && npm run dev` -> starts frontend on port 5173 with live reload
1. open browser at http://localhost:5173

## Architecture
<img src="../readme/rdf-store-architecture.png" alt="architecture diagram">
