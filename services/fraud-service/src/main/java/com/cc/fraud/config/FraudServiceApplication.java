package com.cc.fraud.config;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

@SpringBootApplication(scanBasePackages = "com.cc.fraud")
public class FraudServiceApplication {
  public static void main(String[] args) {
    SpringApplication.run(FraudServiceApplication.class, args);
  }
}
