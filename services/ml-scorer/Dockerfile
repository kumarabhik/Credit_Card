FROM python:3.13-slim

WORKDIR /workspace

COPY services/ml-scorer/requirements.txt /tmp/requirements.txt
RUN python -m pip install --no-cache-dir -r /tmp/requirements.txt

COPY gen/python ./gen/python
COPY services/ml-scorer/README.md ./services/ml-scorer/README.md
COPY services/ml-scorer/app ./services/ml-scorer/app
COPY services/ml-scorer/model ./services/ml-scorer/model

ENV PYTHONPATH=/workspace/gen/python:/workspace/services/ml-scorer/app

CMD ["python", "-m", "ml_scorer.main"]
