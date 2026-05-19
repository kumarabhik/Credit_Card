# ml-scorer

Python gRPC scoring service that trains and serves a local XGBoost model.

Layout:

- `app/ml_scorer/` for training, artifact loading, and the gRPC scorer
- `data/raw/` for local datasets such as Kaggle IEEE-CIS fraud CSVs
- `model/` for generated `model_<sha>.pkl` artifacts and `manifest.json`
- `tests/` for unit tests around dataset prep, artifact loading, and scoring

Current implementation notes:

- The service trains on the local Kaggle IEEE-CIS fraud dataset if present.
- Features are reduced to a numeric schema compatible with the existing proto contract `map<string, double>`.
- The runtime model is loaded from local artifacts at boot and never fetched from the network.
- Inference exposes `ml_scorer_inference_duration_seconds{model_version=...}` via Prometheus.

Train locally:

```powershell
python services/ml-scorer/app/ml_scorer/train.py --data-dir services/ml-scorer/data/raw --model-dir services/ml-scorer/model --max-rows 50000
```

Run locally after training:

```powershell
$env:PYTHONPATH="$PWD\\gen\\python;$PWD\\services\\ml-scorer\\app"
python -m ml_scorer.main
```

Dataset handling:

- Keep Kaggle files out of git.
- Download after accepting Kaggle terms, then place them in `services/ml-scorer/data/raw/`.
- Expected files:
  - `train_transaction.csv`
  - `train_identity.csv`
  - `test_transaction.csv`
  - `test_identity.csv`
  - `sample_submission.csv`
