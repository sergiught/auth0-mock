package example;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertTrue;

import com.nimbusds.jose.JWSAlgorithm;
import com.nimbusds.jose.jwk.source.JWKSource;
import com.nimbusds.jose.jwk.source.JWKSourceBuilder;
import com.nimbusds.jose.proc.JWSVerificationKeySelector;
import com.nimbusds.jose.proc.SecurityContext;
import com.nimbusds.jose.util.JSONObjectUtils;
import com.nimbusds.jwt.JWTClaimsSet;
import com.nimbusds.jwt.proc.DefaultJWTClaimsVerifier;
import com.nimbusds.jwt.proc.DefaultJWTProcessor;
import java.net.URI;
import java.net.URLEncoder;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import java.util.Set;
import org.junit.jupiter.api.Test;
import org.testcontainers.containers.GenericContainer;
import org.testcontainers.containers.wait.strategy.Wait;
import org.testcontainers.junit.jupiter.Container;
import org.testcontainers.junit.jupiter.Testcontainers;

@Testcontainers
class Auth0MockTest {

    // CI sets AUTH0_MOCK_IMAGE to a locally built image; readers get the published one.
    static final String IMAGE =
            System.getenv().getOrDefault("AUTH0_MOCK_IMAGE", "ghcr.io/sergiught/auth0-mock:latest");

    // The extension starts the container before the test and stops it afterwards.
    @Container
    static GenericContainer<?> mock = new GenericContainer<>(IMAGE)
            .withEnv("ADMIN_ALLOW_REMOTE", "true") // the image listens on 0.0.0.0; the mock refuses to start that way without this, since /admin0 has no auth
            .withEnv("HTTPS_ADDR", "off") // plain HTTP, no certificate trust to set up
            .withEnv("ISSUER_URL", "auto") // the issuer follows the URL the test reaches the mock on
            .withExposedPorts(8080)
            // Wait for readiness instead of sleeping.
            .waitingFor(Wait.forHttp("/healthz").forPort(8080).forStatusCode(200));

    static final HttpClient http = HttpClient.newHttpClient();

    static HttpResponse<String> send(HttpRequest.Builder request) throws Exception {
        return http.send(request.build(), HttpResponse.BodyHandlers.ofString());
    }

    static String form(Map<String, String> fields) {
        return fields.entrySet().stream()
                .map(e -> e.getKey() + "=" + URLEncoder.encode(e.getValue(), StandardCharsets.UTF_8))
                .reduce((a, b) -> a + "&" + b)
                .orElse("");
    }

    @Test
    void issuesTokensAndServesStubs() throws Exception {
        // Ask Testcontainers for the host and random mapped port; the host is not always this machine.
        String base = "http://" + mock.getHost() + ":" + mock.getMappedPort(8080);

        // Stub the Management API call your app under test will make.
        String stub = """
                {"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,
                 "body":{"user_id":"auth0|123","email":"alice@example.com","name":"Alice"}}}""";
        var res = send(HttpRequest.newBuilder(URI.create(base + "/admin0/expectations"))
                .header("content-type", "application/json")
                .POST(HttpRequest.BodyPublishers.ofString(stub)));
        assertTrue(res.statusCode() < 300, "stub status " + res.statusCode() + ": " + res.body());

        // Get a token the way your app does.
        res = send(HttpRequest.newBuilder(URI.create(base + "/oauth/token"))
                .header("content-type", "application/x-www-form-urlencoded")
                .POST(HttpRequest.BodyPublishers.ofString(form(Map.of(
                        "grant_type", "client_credentials",
                        "client_id", "demo",
                        "client_secret", "x",
                        "audience", base + "/api/v2/")))));
        assertEquals(200, res.statusCode(), "token status " + res.statusCode() + ": " + res.body());
        String token = (String) JSONObjectUtils.parse(res.body()).get("access_token");
        assertNotNull(token);

        // Call the stubbed endpoint; "|" is percent-encoded so every HTTP client accepts it.
        res = send(HttpRequest.newBuilder(URI.create(base + "/api/v2/users/auth0%7C123"))
                .header("authorization", "Bearer " + token)
                .GET());
        assertEquals(200, res.statusCode(), "user status " + res.statusCode() + ": " + res.body());
        var user = JSONObjectUtils.parse(res.body());
        assertEquals("auth0|123", user.get("user_id"));
        assertEquals("alice@example.com", user.get("email"));

        // Verify the token as a real app would: discovery, JWKS, RS256 signature, issuer and audience.
        var discovery = JSONObjectUtils.parse(
                send(HttpRequest.newBuilder(URI.create(base + "/.well-known/openid-configuration")).GET()).body());
        JWKSource<SecurityContext> jwks =
                JWKSourceBuilder.create(URI.create((String) discovery.get("jwks_uri")).toURL()).build();
        var processor = new DefaultJWTProcessor<>();
        processor.setJWSKeySelector(new JWSVerificationKeySelector<>(JWSAlgorithm.RS256, jwks));
        processor.setJWTClaimsSetVerifier(new DefaultJWTClaimsVerifier<>(
                base + "/api/v2/", new JWTClaimsSet.Builder().issuer(base + "/").build(), Set.of("exp")));
        processor.process(token, null);
    }
}
