# ml-scorer

Python scoring service skeleton.

Expected layout:

- `app/` for the gRPC server and request handling
- `data/` for synthetic training data generation
- `model/` for versioned model artifacts and manifests
- `tests/` for unit and integration tests

The runtime model must be loaded from local artifacts, never fetched over the network during a request.
