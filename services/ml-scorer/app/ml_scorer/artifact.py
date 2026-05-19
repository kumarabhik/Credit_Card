from __future__ import annotations

import hashlib
import json
import pickle
from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path

import numpy as np
import pandas as pd
from sklearn.impute import SimpleImputer
from sklearn.metrics import roc_auc_score
from sklearn.model_selection import train_test_split
from xgboost import XGBClassifier

from ml_scorer.dataset import TrainingDataset
from ml_scorer.schema import MODEL_FEATURE_NAMES, frame_from_feature_map


@dataclass(slots=True)
class ModelBundle:
    feature_names: tuple[str, ...]
    imputer: SimpleImputer
    classifier: XGBClassifier
    model_version: str
    validation_auc: float
    trained_rows: int

    def score_probability(self, features: dict[str, float]) -> float:
        frame = frame_from_feature_map(features)
        transformed = self.imputer.transform(frame)
        probability = float(self.classifier.predict_proba(transformed)[0][1])
        return max(0.0, min(1.0, probability))


def train_bundle(dataset: TrainingDataset, *, random_state: int = 42) -> ModelBundle:
    feature_frame = dataset.features.loc[:, MODEL_FEATURE_NAMES]
    X_train, X_valid, y_train, y_valid = train_test_split(
        feature_frame,
        dataset.labels,
        test_size=0.2,
        random_state=random_state,
        stratify=dataset.labels,
    )

    imputer = SimpleImputer(strategy="median")
    X_train_imputed = imputer.fit_transform(X_train)
    X_valid_imputed = imputer.transform(X_valid)

    classifier = XGBClassifier(
        objective="binary:logistic",
        eval_metric="auc",
        n_estimators=300,
        max_depth=6,
        learning_rate=0.1,
        subsample=0.8,
        colsample_bytree=0.8,
        tree_method="hist",
        random_state=random_state,
        n_jobs=4,
    )
    classifier.fit(
        X_train_imputed,
        y_train,
        eval_set=[(X_valid_imputed, y_valid)],
        verbose=False,
    )

    probabilities = classifier.predict_proba(X_valid_imputed)[:, 1]
    validation_auc = float(roc_auc_score(y_valid, probabilities))
    version = _build_model_version(classifier, imputer, feature_frame.columns)
    return ModelBundle(
        feature_names=tuple(feature_frame.columns),
        imputer=imputer,
        classifier=classifier,
        model_version=version,
        validation_auc=validation_auc,
        trained_rows=dataset.source_rows,
    )


def save_bundle(bundle: ModelBundle, model_dir: Path, *, source: str) -> Path:
    model_dir.mkdir(parents=True, exist_ok=True)

    artifact_path = model_dir / f"model_{bundle.model_version}.pkl"
    artifact_bytes = pickle.dumps(
        {
            "feature_names": bundle.feature_names,
            "imputer": bundle.imputer,
            "classifier": bundle.classifier,
            "model_version": bundle.model_version,
            "validation_auc": bundle.validation_auc,
            "trained_rows": bundle.trained_rows,
        },
        protocol=pickle.HIGHEST_PROTOCOL,
    )
    artifact_path.write_bytes(artifact_bytes)

    manifest = {
        "model_version": bundle.model_version,
        "artifact_file": artifact_path.name,
        "feature_names": list(bundle.feature_names),
        "validation_auc": round(bundle.validation_auc, 6),
        "trained_rows": bundle.trained_rows,
        "source": source,
        "created_at_utc": datetime.now(UTC).isoformat(),
    }
    (model_dir / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    return artifact_path


def load_bundle(model_dir: Path) -> ModelBundle:
    manifest_path = model_dir / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    payload = pickle.loads((model_dir / manifest["artifact_file"]).read_bytes())
    return ModelBundle(
        feature_names=tuple(payload["feature_names"]),
        imputer=payload["imputer"],
        classifier=payload["classifier"],
        model_version=payload["model_version"],
        validation_auc=float(payload["validation_auc"]),
        trained_rows=int(payload["trained_rows"]),
    )


def probability_to_risk_score(probability: float) -> int:
    return int(round(max(0.0, min(1.0, probability)) * 1000.0))


def _build_model_version(
    classifier: XGBClassifier,
    imputer: SimpleImputer,
    feature_columns: pd.Index,
) -> str:
    payload = json.dumps(
        {
            "feature_names": list(feature_columns),
            "params": classifier.get_xgb_params(),
            "imputer_statistics": np.nan_to_num(imputer.statistics_, nan=-1.0).tolist(),
        },
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()[:12]
