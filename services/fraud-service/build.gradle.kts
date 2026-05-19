plugins {
    java
    application
    id("org.springframework.boot") version "3.4.0"
    id("io.spring.dependency-management") version "1.1.6"
}

group = "com.cc"
version = "0.1.0"

java {
    toolchain {
        languageVersion.set(JavaLanguageVersion.of(21))
    }
}

repositories {
    mavenCentral()
}

sourceSets {
    main {
        java {
            srcDir("../../gen/java")
        }
    }
}

dependencies {
    implementation("org.springframework.boot:spring-boot-starter")
    implementation("org.springframework.boot:spring-boot-starter-webflux")
    implementation("io.grpc:grpc-netty-shaded:1.67.1")
    implementation("io.grpc:grpc-protobuf:1.67.1")
    implementation("io.grpc:grpc-stub:1.67.1")
    implementation("io.projectreactor:reactor-core")

    testImplementation("org.springframework.boot:spring-boot-starter-test")
    testImplementation("io.projectreactor:reactor-test")
}

application {
    mainClass.set("com.cc.fraud.config.FraudServiceApplication")
}

tasks.test {
    useJUnitPlatform()
}
