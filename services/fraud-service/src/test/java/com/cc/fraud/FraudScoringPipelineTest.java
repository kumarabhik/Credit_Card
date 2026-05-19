package com.cc.fraud;

import static org.assertj.core.api.Assertions.assertThat;

import com.cc.common.v1.Decision;
import com.cc.common.v1.Money;
import com.cc.fraud.scoring.FraudScoringPipeline;
import com.cc.fraud.v1.ScoreRequest;
import org.junit.jupiter.api.Test;
import reactor.test.StepVerifier;

class FraudScoringPipelineTest {
  @Test
  void scoreBuildsReactiveRiskView() {
    FraudScoringPipeline pipeline = new FraudScoringPipeline();

    StepVerifier.create(
            pipeline.score(
                ScoreRequest.newBuilder()
                    .setTxnId("txn-demo")
                    .setAccountId("acct-demo")
                    .setCardToken("tok_demo_card")
                    .setMerchantId("mch-demo")
                    .setChannel("CARD_NOT_PRESENT")
                    .setDeviceId("device-1")
                    .setAmount(Money.newBuilder().setCurrency("USD").setMinorUnits(15_000).build())
                    .build()))
        .assertNext(
            scoreView -> {
              assertThat(scoreView.riskScore()).isEqualTo(57);
              assertThat(scoreView.decision()).isEqualTo(Decision.DECISION_APPROVE);
              assertThat(scoreView.modelVersion()).isEqualTo("rules-v1");
              assertThat(scoreView.reasonCodes()).containsExactly("CHANNEL_CNP", "DEVICE_HIGH_TICKET");
            })
        .verifyComplete();
  }
}
