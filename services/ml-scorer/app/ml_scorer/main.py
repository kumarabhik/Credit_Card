from __future__ import annotations

import asyncio
import os
from pathlib import Path

import grpc

from ml.v1 import score_pb2_grpc
from ml_scorer.artifact import load_bundle
from ml_scorer.log_config import configure_logging
from ml_scorer.service import MLScoreService, start_metrics_server


async def serve() -> None:
    logger = configure_logging()
    grpc_addr = os.getenv("ML_SCORER_GRPC_ADDR", "0.0.0.0:9094")
    metrics_port = int(os.getenv("ML_SCORER_METRICS_PORT", "9104"))
    model_dir = Path(os.getenv("ML_SCORER_MODEL_DIR", "services/ml-scorer/model")).resolve()

    bundle = load_bundle(model_dir)
    start_metrics_server(metrics_port)

    server = grpc.aio.server()
    score_pb2_grpc.add_MLScoreServiceServicer_to_server(MLScoreService(bundle, logger), server)
    server.add_insecure_port(grpc_addr)

    logger.info(
        "starting ml-scorer",
        grpc_addr=grpc_addr,
        metrics_port=metrics_port,
        model_version=bundle.model_version,
        model_dir=str(model_dir),
    )
    await server.start()
    await server.wait_for_termination()


def main() -> None:
    asyncio.run(serve())


if __name__ == "__main__":
    main()
