package app;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import org.junit.jupiter.api.Test;

class HealthE2E {
  static final String BASE = System.getenv().getOrDefault("BASE_URL", "http://localhost:8080");

  @Test
  void scaffoldRunningAppIsHealthy() throws Exception {
    HttpResponse<String> res =
        HttpClient.newHttpClient()
            .send(
                HttpRequest.newBuilder(URI.create(BASE + "/actuator/health")).build(),
                HttpResponse.BodyHandlers.ofString());
    assertEquals(200, res.statusCode());
  }
}
