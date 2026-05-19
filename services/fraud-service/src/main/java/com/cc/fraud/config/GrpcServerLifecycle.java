package com.cc.fraud.config;

import com.cc.fraud.grpc.FraudGrpcService;
import io.grpc.Server;
import io.grpc.netty.shaded.io.grpc.netty.NettyServerBuilder;
import java.io.IOException;
import java.util.concurrent.TimeUnit;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.SmartLifecycle;
import org.springframework.stereotype.Component;

@Component
public class GrpcServerLifecycle implements SmartLifecycle {
  private static final Logger log = LoggerFactory.getLogger(GrpcServerLifecycle.class);

  private final FraudGrpcService fraudGrpcService;
  private final int port;
  private volatile boolean running;
  private Server server;

  public GrpcServerLifecycle(
      FraudGrpcService fraudGrpcService,
      @Value("${fraud.grpc.port:9094}") int port) {
    this.fraudGrpcService = fraudGrpcService;
    this.port = port;
  }

  @Override
  public void start() {
    if (running) {
      return;
    }

    try {
      server = NettyServerBuilder.forPort(port).addService(fraudGrpcService).build().start();
      running = true;
      log.info("fraud gRPC server started on port {}", port);
    } catch (IOException exception) {
      throw new IllegalStateException("failed to start fraud gRPC server", exception);
    }
  }

  @Override
  public void stop() {
    if (!running || server == null) {
      return;
    }

    server.shutdown();
    try {
      server.awaitTermination(5, TimeUnit.SECONDS);
    } catch (InterruptedException exception) {
      Thread.currentThread().interrupt();
    }
    running = false;
    log.info("fraud gRPC server stopped");
  }

  @Override
  public boolean isRunning() {
    return running;
  }

  @Override
  public int getPhase() {
    return Integer.MAX_VALUE;
  }
}
