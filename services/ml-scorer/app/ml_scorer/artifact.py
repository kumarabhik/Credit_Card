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
from sklearn.metrics import (
    average_precision_score,
    confusion_matrix,
    f1_score,
    precision_recall_curve,
    precision_score,
    recall_score,
    roc_auc_score,
)
from sklearn.model_selection import train_test_split
from xgboost import XGBClassifier

from ml_scorer.dataset import TrainingDataset
from ml_scorer.rebalance import ResampleSummary, apply_smote
from ml_scorer.schema import MODEL_FEATURE_NAMES, vector_from_feature_map


@dataclass(slots=True)
class TrainingReport:
    validation_auc: float
    average_precision: float
    precision: float
    recall: float
    f1: float
    decision_threshold: float
    validation_rows: int
    smote_applied: bool
    original_positive_rate: float
    balanced_positive_rate: float
    original_rows: int
    balanced_rows: int
    true_negative: int
    false_positive: int
    false_negative: int
    true_positive: int

    def as_dict(self) -> dict[str, float | int | bool]:
        return {
            "validation_auc": round(self.validation_auc, 6),
            "average_precision": round(self.average_precision, 6),
            "precision": round(self.precision, 6),
            "recall": round(self.recall, 6),
            "f1": round(self.f1, 6),
            "decision_threshold": round(self.decision_threshold, 6),
            "validation_rows": self.validation_rows,
            "smote_applied": self.smote_applied,
            "original_positive_rate": round(self.original_positive_rate, 6),
            "balanced_positive_rate": round(self.balanced_positive_rate, 6),
            "original_rows": self.original_rows,
            "balanced_rows": self.balanced_rows,
            "true_negative": self.true_negative,
            "false_positive": self.false_positive,
            "false_negative": self.false_negative,
            "true_positive": self.true_positive,
        }


@dataclass(slots=True)
class ModelBundle:
    feature_names: tuple[str, ...]
    imputer: SimpleImputer
    classifier: XGBClassifier
    model_version: str
    validation_auc: float
    trained_rows: int
    training_report: TrainingReport

    def score_probability(self, features: dict[str, float]) -> float:
        vector = vector_from_feature_map(features)
        transformed = self.imputer.transform(vector)
        probability = float(self.classifier.predict_proba(transformed)[0][1])
        return max(0.0, min(1.0, probability))


def train_bundle(
    dataset: TrainingDataset,
    *,
    random_state: int = 42,
    balance_mode: str = "smote",
) -> ModelBundle:
    feature_frame = dataset.features.loc[:, MODEL_FEATURE_NAMES]
    X_train, X_valid, y_train, y_valid = train_test_split(
        feature_frame,
        dataset.labels,
        test_size=0.2,
        random_state=random_state,
        stratify=dataset.labels,
    )

    imputer = SimpleImputer(strategy="median")
    X_train_matrix = X_train.to_numpy(dtype=float, copy=False)
    X_valid_matrix = X_valid.to_numpy(dtype=float, copy=False)
    X_train_imputed = imputer.fit_transform(X_train_matrix)
    X_valid_imputed = imputer.transform(X_valid_matrix)
    if balance_mode == "smote":
        X_train_balanced, y_train_balanced, resample_summary = apply_smote(
            X_train_imputed,
            y_train.to_numpy(),
            random_state=random_state,
        )
    else:
        X_train_balanced = X_train_imputed
        y_train_balanced = y_train.to_numpy()
        positive_rate = float(np.mean(y_train_balanced)) if y_train_balanced.size else 0.0
        resample_summary = ResampleSummary(
            applied=False,
            original_positive_rate=positive_rate,
            balanced_positive_rate=positive_rate,
            original_rows=int(y_train.shape[0]),
            balanced_rows=int(y_train.shape[0]),
        )

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
        X_train_balanced,
        y_train_balanced,
        eval_set=[(X_valid_imputed, y_valid)],
        verbose=False,
    )

    probabilities = classifier.predict_proba(X_valid_imputed)[:, 1]
    validation_auc = float(roc_auc_score(y_valid, probabilities))
    threshold = _optimal_threshold(y_valid.to_numpy(), probabilities)
    predictions = (probabilities >= threshold).astype(int)
    tn, fp, fn, tp = confusion_matrix(y_valid, predictions, labels=[0, 1]).ravel()
    training_report = TrainingReport(
        validation_auc=validation_auc,
        average_precision=float(average_precision_score(y_valid, probabilities)),
        precision=float(precision_score(y_valid, predictions, zero_division=0)),
        recall=float(recall_score(y_valid, predictions, zero_division=0)),
        f1=float(f1_score(y_valid, predictions, zero_division=0)),
        decision_threshold=threshold,
        validation_rows=int(y_valid.shape[0]),
        smote_applied=resample_summary.applied,
        original_positive_rate=resample_summary.original_positive_rate,
        balanced_positive_rate=resample_summary.balanced_positive_rate,
        original_rows=resample_summary.original_rows,
        balanced_rows=resample_summary.balanced_rows,
        true_negative=int(tn),
        false_positive=int(fp),
        false_negative=int(fn),
        true_positive=int(tp),
    )
    version = _build_model_version(classifier, imputer, feature_frame.columns, training_report)
    return ModelBundle(
        feature_names=tuple(feature_frame.columns),
        imputer=imputer,
        classifier=classifier,
        model_version=version,
        validation_auc=validation_auc,
        trained_rows=dataset.source_rows,
        training_report=training_report,
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
            "training_report": bundle.training_report.as_dict(),
        },
        protocol=pickle.HIGHEST_PROTOCOL,
    )
    artifact_path.write_bytes(artifact_bytes)

    report_file = "training_report.json"
    (model_dir / report_file).write_text(
        json.dumps(bundle.training_report.as_dict(), indent=2),
        encoding="utf-8",
    )

    manifest = {
        "model_version": bundle.model_version,
        "artifact_file": artifact_path.name,
        "feature_names": list(bundle.feature_names),
        "validation_auc": round(bundle.validation_auc, 6),
        "average_precision": round(bundle.training_report.average_precision, 6),
        "decision_threshold": round(bundle.training_report.decision_threshold, 6),
        "smote_applied": bundle.training_report.smote_applied,
        "trained_rows": bundle.trained_rows,
        "source": source,
        "training_report_file": report_file,
        "created_at_utc": datetime.now(UTC).isoformat(),
    }
    (model_dir / "manifest.json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    return artifact_path


def load_bundle(model_dir: Path) -> ModelBundle:
    manifest_path = model_dir / "manifest.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    payload = pickle.loads((model_dir / manifest["artifact_file"]).read_bytes())
    report_payload = payload.get("training_report") or {
        "validation_auc": float(payload["validation_auc"]),
        "average_precision": float(payload["validation_auc"]),
        "precision": 0.0,
        "recall": 0.0,
        "f1": 0.0,
        "decision_threshold": 0.5,
        "validation_rows": 0,
        "smote_applied": False,
        "original_positive_rate": 0.0,
        "balanced_positive_rate": 0.0,
        "original_rows": int(payload["trained_rows"]),
        "balanced_rows": int(payload["trained_rows"]),
        "true_negative": 0,
        "false_positive": 0,
        "false_negative": 0,
        "true_positive": 0,
    }
    return ModelBundle(
        feature_names=tuple(payload["feature_names"]),
        imputer=payload["imputer"],
        classifier=payload["classifier"],
        model_version=payload["model_version"],
        validation_auc=float(payload["validation_auc"]),
        trained_rows=int(payload["trained_rows"]),
        training_report=TrainingReport(**report_payload),
    )


def probability_to_risk_score(probability: float) -> int:
    return int(round(max(0.0, min(1.0, probability)) * 1000.0))


def _build_model_version(
    classifier: XGBClassifier,
    imputer: SimpleImputer,
    feature_columns: pd.Index,
    training_report: TrainingReport,
) -> str:
    payload = json.dumps(
        {
            "feature_names": list(feature_columns),
            "params": classifier.get_xgb_params(),
            "imputer_statistics": np.nan_to_num(imputer.statistics_, nan=-1.0).tolist(),
            "decision_threshold": round(training_report.decision_threshold, 6),
            "smote_applied": training_report.smote_applied,
        },
        sort_keys=True,
    ).encode("utf-8")
    return hashlib.sha256(payload).hexdigest()[:12]


def _optimal_threshold(labels: np.ndarray, probabilities: np.ndarray) -> float:
    precision, recall, thresholds = precision_recall_curve(labels, probabilities)
    if thresholds.size == 0:
        return 0.5
    f1_scores = (2.0 * precision[:-1] * recall[:-1]) / np.maximum(precision[:-1] + recall[:-1], 1e-9)
    best_index = int(np.nanargmax(f1_scores))
    return float(thresholds[best_index])
