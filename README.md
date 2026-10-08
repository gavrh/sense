# sense

**System One Search Engine**: retrieval-first question answering that uses fast, non-autoregressive
decision models to pick the best source and the exact passage, then returns a short, cited answer

## How it works

1. A query is submitted from the page
2. SearXNG is queried and its HTML results are parsed into candidates
3. The decision model ranks the candidates and picks the one most likely to contain the answer
4. That page is fetched and distilled into readable passages
5. The model selects the passage, then the sentence within it, that answers the query
6. The short quoted answer is returned with its source

The decision engine is not generative: it scores options and returns calibrated probabilities, and the
answer text always comes from the cited page

## Status

sense is in early development and is being actively built. The core flow works today: a query is
answered by ranking sources, fetching the chosen page, and quoting the passage that answers it. The
specific workflow for producing progressively better answers is still being tested and optimized, as
are ranking quality, multi-source search, caching, and deployment, so expect rough edges

There is no setup guide yet

## License

AGPL-3.0
