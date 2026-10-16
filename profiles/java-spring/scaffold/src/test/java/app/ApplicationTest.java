package app;

import static org.junit.jupiter.api.Assertions.assertEquals;

import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.web.server.LocalServerPort;
import org.springframework.jdbc.core.JdbcTemplate;

@SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.RANDOM_PORT)
class ApplicationTest {
  @LocalServerPort int port;

  @Autowired JdbcTemplate jdbc;

  @Test
  void scaffoldAnswersItsHealthCheck() throws Exception {
    HttpResponse<String> res =
        HttpClient.newHttpClient()
            .send(
                HttpRequest.newBuilder(URI.create("http://localhost:" + port + "/actuator/health"))
                    .build(),
                HttpResponse.BodyHandlers.ofString());
    assertEquals(200, res.statusCode());
  }

  @Test
  void scaffoldReachesTheDatabase() {
    assertEquals(1, jdbc.queryForObject("SELECT 1", Integer.class));
  }
}
