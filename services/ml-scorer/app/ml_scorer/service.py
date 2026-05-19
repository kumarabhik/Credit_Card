from __future__ import annotations

import time
from collections.abc import Mapping

from prometheus_client import Histogram, start_http_server

from ml.v1 import score_pb2, score_pb2_grpc
from ml_scorer.artifact import ModelBundle, probability_to_risk_score

INFERENCE_DURATION = Histogram(
    "ml_scorer_inference_duration_seconds",
    "Latency of ML scorer inference requests.",
    labelnames=("model_version",),
)


class MLScoreService(score_pb2_grpc.MLScoreServiceServicer):
    def __init__(self, bundle: ModelBundle, logger) -> None:
        self._bundle = bundle
        self._logger = logger

    async def Score(self, request: score_pb2.ScoreRequest, context) -> score_pb2.ScoreResponse:  # noqa: N802
        started = time.perf_counter()
        probability = self._bundle.score_probability(dict(request.features))
        risk_score = probability_to_risk_score(probability)
        elapsed = time.perf_counter() - started

        INFERENCE_DURATION.labels(model_version=self._bundle.model_version).observe(elapsed)
        self._logger.info(
            "ml score computed",
            model_version=self._bundle.model_version,
            risk_score=risk_score,
            feature_count=len(request.features),
            inference_seconds=round(elapsed, 6),
        )
        return score_pb2.ScoreResponse(
            risk_score=risk_score,
            model_version=self._bundle.model_version,
        )


def start_metrics_server(metrics_port: int) -> None:
    start_http_server(metrics_port)


def score_locally(bundle: ModelBundle, features: Mapping[str, float]) -> score_pb2.ScoreResponse:
    probability = bundle.score_probability(dict(features))
    return score_pb2.ScoreResponse(
        risk_score=probability_to_risk_score(probability),
        model_version=bundle.model_version,
    )
