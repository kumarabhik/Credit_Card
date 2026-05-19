package com.cc.fraud.scoring;

import com.cc.common.v1.Decision;
import com.cc.fraud.v1.RuleVerdict;
import java.util.List;

public record FraudScoreView(
    int riskScore,
    Decision decision,
    List<RuleVerdict> verdicts,
    String modelVersion,
    List<String> reasonCodes) {}
