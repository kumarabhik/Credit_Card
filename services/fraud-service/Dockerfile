FROM gradle:8.10.2-jdk21 AS build

WORKDIR /workspace

COPY gen/java ./gen/java
COPY services/fraud-service ./services/fraud-service

RUN gradle --no-daemon -p services/fraud-service clean test bootJar

FROM eclipse-temurin:21-jre

WORKDIR /app

COPY --from=build /workspace/services/fraud-service/build/libs/fraud-service-0.1.0.jar /app/fraud-service.jar

EXPOSE 9094

ENTRYPOINT ["java", "-jar", "/app/fraud-service.jar"]
