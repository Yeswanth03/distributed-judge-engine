# Catalog service: build with the Maven wrapper, run on a JRE as a non-root user.

FROM eclipse-temurin:21-jdk-noble AS build
WORKDIR /src
# Dependencies first, so a code change doesn't re-download them.
COPY mvnw pom.xml ./
COPY .mvn .mvn
RUN ./mvnw -B -q dependency:go-offline
COPY src src
RUN ./mvnw -B -q package -DskipTests

FROM eclipse-temurin:21-jre-noble
RUN useradd --system --uid 10002 catalog
WORKDIR /app
COPY --from=build /src/target/catalog-service.jar app.jar
USER catalog
EXPOSE 8080
ENTRYPOINT ["java", "-XX:MaxRAMPercentage=75", "-jar", "app.jar"]
