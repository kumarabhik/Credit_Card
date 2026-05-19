package com.cc.fraud.grpc;

import com.cc.common.v1.Decision;
import com.cc.fraud.scoring.FraudScoreView;
import com.cc.fraud.scoring.FraudScoringPipeline;
import com.cc.fraud.v1.FraudServiceGrpc;
import com.cc.fraud.v1.RuleVerdict;
import com.cc.fraud.v1.ScoreRequest;
import com.cc.fraud.v1.ScoreResponse;
import io.grpc.Status;
import io.grpc.stub.StreamObserver;
import java.util.List;
import org.springframework.stereotype.Service;

@Service
public class FraudGrpcService extends FraudServiceGrpc.FraudServiceImplBase {
  private final FraudScoringPipeline pipeline;

  public FraudGrpcService(FraudScoringPipeline pipeline) {
    this.pipeline = pipeline;
  }

  @Override
  public void score(ScoreRequest request, StreamObserver<ScoreResponse> responseObserver) {
    pipeline
        .score(request)
        .map(this::toResponse)
        .subscribe(
            response -> {
              responseObserver.onNext(response);
              responseObserver.onCompleted();
            },
            error ->
                responseObserver.onError(
                    Status.INTERNAL
                        .withDescription("fraud scoring failed")
                        .withCause(error)
                        .asRuntimeException()));
  }

  private ScoreResponse toResponse(FraudScoreView scoreView) {
    return ScoreResponse.newBuilder()
        .setRiskScore(scoreView.riskScore())
        .setDecision(scoreView.decision())
        .addAllVerdicts(scoreView.verdicts())
        .setModelVersion(scoreView.modelVersion())
        .addAllReasonCodes(scoreView.reasonCodes())
        .build();
  }
}
