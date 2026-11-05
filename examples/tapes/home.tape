# examples/tapes/home.tape — docs site home page demo: stub a Management API
# response, mint a real RS256 token, look inside it, and call the stub with it.
# Regenerate: make recordings
Output docs/site/public/recordings/home.webm

Require curl
Require jq

Set Shell "bash"
Set FontFamily "MesloLGM Nerd Font Mono"
Set FontSize 20
Set Width 1400
Set Height 620
Set Padding 20
Set TypingSpeed 50ms
Set Theme "Catppuccin Mocha"

# --- hidden setup: boot the freshly built mock, HTTP only on :8080, and wait for it ---
Hide
Type "HTTPS_ADDR=off HTTP_ADDR=127.0.0.1:8080 ./bin/auth0-mock > /tmp/auth0-mock-recording.log 2>&1 &" Enter
Type "until curl -s localhost:8080/healthz >/dev/null; do sleep 0.2; done" Enter
Sleep 1s
Type "clear" Enter
Show

# 1. Stub a Management API response.
Sleep 500ms
Type "# Stub a Management API response" Enter
Type@15ms `curl -s localhost:8080/admin0/expectations -H 'Content-Type: application/json' -d '{"method":"GET","path":"/api/v2/users/auth0|123","response":{"status":200,"body":{"user_id":"auth0|123","email":"alice@example.com","name":"Alice"}}}'` Sleep 400ms Enter
Sleep 2s

# 2. Mint a token and decode its claims.
Hide
Type "clear" Enter
Show
Type "# Mint a real RS256 token" Enter
Type@20ms "TOKEN=$(curl -s localhost:8080/oauth/token -d grant_type=client_credentials -d client_id=demo -d client_secret=x -d audience=http://localhost:8080/api/v2/ | jq -r .access_token)" Sleep 400ms Enter
Sleep 800ms
Type "# Look inside it" Enter
Type `jq -R 'split(".")[1] | @base64d | fromjson' <<< "$TOKEN"` Sleep 400ms Enter
Sleep 3500ms

# 3. Call the stubbed endpoint with the token.
Hide
Type "clear" Enter
Show
Type "# Call the stubbed endpoint with it" Enter
Type `curl -s localhost:8080/api/v2/users/auth0%7C123 -H "Authorization: Bearer $TOKEN" | jq` Sleep 400ms Enter
Sleep 3500ms

# --- hidden teardown ---
Hide
Type "kill %1" Enter
Sleep 300ms
