package com.cc.fraud.scoring;

import com.cc.common.v1.Decision;
import com.cc.fraud.v1.RuleVerdict;
import com.cc.fraud.v1.ScoreRequest;
import java.util.ArrayList;
import java.util.List;
import org.springframework.stereotype.Component;
import reactor.core.publisher.Mono;

@Component
public class FraudScoringPipeline {
  public Mono<FraudScoreView> score(ScoreRequest request) {
    return Mono.fromSupplier(() -> evaluate(request));
  }

  private FraudScoreView evaluate(ScoreRequest request) {
    List<RuleVerdict> verdicts = new ArrayList<>();
    int score = 0;

    if ("CARD_NOT_PRESENT".equalsIgnoreCase(request.getChannel())) {
      verdicts.add(
          RuleVerdict.newBuilder()
              .setCode("CHANNEL_CNP")
              .setHit(true)
              .setWeight(35)
              .setReason("card-not-present channel")
              .build());
      score += 35;
    }

    if (!request.getDeviceId().isBlank() && request.getAmount().getMinorUnits() > 10_000) {
      verdicts.add(
          RuleVerdict.newBuilder()
              .setCode("DEVICE_HIGH_TICKET")
              .setHit(true)
              .setWeight(22)
              .setReason("large amount on device-bound request")
              .build());
      score += 22;
    }

    Decision decision = score >= 70 ? Decision.DECISION_REVIEW : Decision.DECISION_APPROVE;
    List<String> reasonCodes =
        verdicts.stream().filter(RuleVerdict::getHit).map(RuleVerdict::getCode).toList();

    return new FraudScoreView(score, decision, verdicts, "rules-v1", reasonCodes);
  }
}
